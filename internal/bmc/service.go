package bmc

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"shoal/internal/database"
	"shoal/pkg/models"
)

// Service handles BMC communication and management
type Service struct {
	db         *database.DB
	client     *http.Client
	idCache    map[string]*bmcIDCache // Cache for discovered IDs per BMC
	idCacheMux sync.RWMutex
}

// bmcIDCache stores discovered manager and system IDs for a BMC
type bmcIDCache struct {
	managerID string
	systemID  string
	cachedAt  time.Time
}

// New creates a new BMC service
func New(db *database.DB) *Service {
	// Create HTTP client with timeout and insecure TLS for BMCs
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // BMCs often use self-signed certificates
			},
		},
	}

	return &Service{
		db:      db,
		client:  client,
		idCache: make(map[string]*bmcIDCache),
	}
}

// ProxyRequest forwards a request to the appropriate BMC and returns the response
func (s *Service) ProxyRequest(ctx context.Context, bmcName, path string, r *http.Request) (*http.Response, error) {
	// Get BMC information from database
	bmc, err := s.db.GetBMCByName(ctx, bmcName)
	if err != nil {
		return nil, fmt.Errorf("failed to get BMC: %w", err)
	}
	if bmc == nil {
		return nil, fmt.Errorf("BMC not found: %s", bmcName)
	}
	if !bmc.Enabled {
		return nil, fmt.Errorf("BMC is disabled: %s", bmcName)
	}

	// Construct target URL
	targetURL, err := s.buildBMCURL(bmc, path)
	if err != nil {
		return nil, fmt.Errorf("failed to build BMC URL: %w", err)
	}

	// Create proxy request
	proxyReq, err := s.createProxyRequest(r, targetURL, bmc)
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy request: %w", err)
	}

	// Execute request
	slog.Debug("Proxying request to BMC", "bmc", bmcName, "url", targetURL, "method", r.Method)
	resp, err := s.client.Do(proxyReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute BMC request: %w", err)
	}

	// Update last seen timestamp
	if err := s.db.UpdateBMCLastSeen(ctx, bmc.ID); err != nil {
		slog.Warn("Failed to update BMC last seen", "bmc", bmcName, "error", err)
	}

	return resp, nil
}

// GetFirstManagerID discovers the first manager ID from a BMC
func (s *Service) GetFirstManagerID(ctx context.Context, bmcName string) (string, error) {
	// Check cache first
	s.idCacheMux.RLock()
	if cache, ok := s.idCache[bmcName]; ok && cache.managerID != "" {
		// Cache entries are valid for 5 minutes
		if time.Since(cache.cachedAt) < 5*time.Minute {
			managerID := cache.managerID
			s.idCacheMux.RUnlock()
			return managerID, nil
		}
	}
	s.idCacheMux.RUnlock()

	bmc, err := s.db.GetBMCByName(ctx, bmcName)
	if err != nil {
		return "", fmt.Errorf("failed to get BMC: %w", err)
	}
	if bmc == nil {
		return "", fmt.Errorf("BMC not found: %s", bmcName)
	}

	// Get managers collection
	managersURL, err := s.buildBMCURL(bmc, "/redfish/v1/Managers")
	if err != nil {
		return "", fmt.Errorf("failed to build managers URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", managersURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.SetBasicAuth(bmc.Username, bmc.Password)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get managers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get managers collection: status %d", resp.StatusCode)
	}

	// Parse managers collection
	var collection struct {
		Members []struct {
			ODataID string `json:"@odata.id"`
		} `json:"Members"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&collection); err != nil {
		return "", fmt.Errorf("failed to parse managers collection: %w", err)
	}

	if len(collection.Members) == 0 {
		return "", fmt.Errorf("no managers found on BMC")
	}

	// Extract manager ID from the first member's OData ID
	// Format is typically /redfish/v1/Managers/{ManagerId}
	parts := strings.Split(collection.Members[0].ODataID, "/")
	if len(parts) >= 5 {
		managerID := parts[4]

		// Update cache
		s.idCacheMux.Lock()
		if s.idCache[bmcName] == nil {
			s.idCache[bmcName] = &bmcIDCache{}
		}
		s.idCache[bmcName].managerID = managerID
		s.idCache[bmcName].cachedAt = time.Now()
		s.idCacheMux.Unlock()

		return managerID, nil
	}

	return "", fmt.Errorf("unable to parse manager ID from %s", collection.Members[0].ODataID)
}

// GetFirstSystemID discovers the first system ID from a BMC
func (s *Service) GetFirstSystemID(ctx context.Context, bmcName string) (string, error) {
	// Check cache first
	s.idCacheMux.RLock()
	if cache, ok := s.idCache[bmcName]; ok && cache.systemID != "" {
		// Cache entries are valid for 5 minutes
		if time.Since(cache.cachedAt) < 5*time.Minute {
			systemID := cache.systemID
			s.idCacheMux.RUnlock()
			return systemID, nil
		}
	}
	s.idCacheMux.RUnlock()

	bmc, err := s.db.GetBMCByName(ctx, bmcName)
	if err != nil {
		return "", fmt.Errorf("failed to get BMC: %w", err)
	}
	if bmc == nil {
		return "", fmt.Errorf("BMC not found: %s", bmcName)
	}

	// Get systems collection
	systemsURL, err := s.buildBMCURL(bmc, "/redfish/v1/Systems")
	if err != nil {
		return "", fmt.Errorf("failed to build systems URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", systemsURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.SetBasicAuth(bmc.Username, bmc.Password)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get systems: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get systems collection: status %d", resp.StatusCode)
	}

	// Parse systems collection
	var collection struct {
		Members []struct {
			ODataID string `json:"@odata.id"`
		} `json:"Members"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&collection); err != nil {
		return "", fmt.Errorf("failed to parse systems collection: %w", err)
	}

	if len(collection.Members) == 0 {
		return "", fmt.Errorf("no systems found on BMC")
	}

	// Extract system ID from the first member's OData ID
	// Format is typically /redfish/v1/Systems/{SystemId}
	parts := strings.Split(collection.Members[0].ODataID, "/")
	if len(parts) >= 5 {
		systemID := parts[4]

		// Update cache
		s.idCacheMux.Lock()
		if s.idCache[bmcName] == nil {
			s.idCache[bmcName] = &bmcIDCache{}
		}
		s.idCache[bmcName].systemID = systemID
		s.idCache[bmcName].cachedAt = time.Now()
		s.idCacheMux.Unlock()

		return systemID, nil
	}

	return "", fmt.Errorf("unable to parse system ID from %s", collection.Members[0].ODataID)
}

// PowerControl executes a power control action on a BMC
func (s *Service) PowerControl(ctx context.Context, bmcName string, action models.PowerAction) error {
	bmc, err := s.db.GetBMCByName(ctx, bmcName)
	if err != nil {
		return fmt.Errorf("failed to get BMC: %w", err)
	}
	if bmc == nil {
		return fmt.Errorf("BMC not found: %s", bmcName)
	}
	if !bmc.Enabled {
		return fmt.Errorf("BMC is disabled: %s", bmcName)
	}

	// First, get the list of available systems to find the correct system ID
	systemsURL, err := s.buildBMCURL(bmc, "/redfish/v1/Systems")
	if err != nil {
		return fmt.Errorf("failed to build systems URL: %w", err)
	}

	// Get systems collection
	systemsReq, err := http.NewRequestWithContext(ctx, "GET", systemsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create systems request: %w", err)
	}
	systemsReq.SetBasicAuth(bmc.Username, bmc.Password)
	systemsReq.Header.Set("Accept", "application/json")

	systemsResp, err := s.client.Do(systemsReq)
	if err != nil {
		return fmt.Errorf("failed to get systems: %w", err)
	}
	defer systemsResp.Body.Close()

	if systemsResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(systemsResp.Body)
		return fmt.Errorf("failed to get systems collection: status %d: %s", systemsResp.StatusCode, string(body))
	}

	// Parse systems collection to get the first system ID
	var systemsCollection struct {
		Members []struct {
			ODataID string `json:"@odata.id"`
		} `json:"Members"`
	}

	if err := json.NewDecoder(systemsResp.Body).Decode(&systemsCollection); err != nil {
		return fmt.Errorf("failed to parse systems collection: %w", err)
	}

	if len(systemsCollection.Members) == 0 {
		return fmt.Errorf("no systems found on BMC")
	}

	// Use the first system's ID for power control
	// The OData ID format is typically /redfish/v1/Systems/{SystemId}
	systemPath := systemsCollection.Members[0].ODataID
	powerActionPath := systemPath + "/Actions/ComputerSystem.Reset"

	slog.Debug("Using system for power control", "systemPath", systemPath, "actionPath", powerActionPath)

	// Create power control request
	powerReq := models.PowerRequest{ResetType: action}
	body, err := json.Marshal(powerReq)
	if err != nil {
		return fmt.Errorf("failed to marshal power request: %w", err)
	}

	// Construct power control URL using the discovered path
	targetURL, err := s.buildBMCURL(bmc, powerActionPath)
	if err != nil {
		return fmt.Errorf("failed to build BMC URL: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create power control request: %w", err)
	}

	// Set headers and authentication
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(bmc.Username, bmc.Password)

	// Log detailed request information for debugging
	slog.Info("Power control request details",
		"bmc", bmcName,
		"action", action,
		"targetURL", targetURL,
		"systemPath", systemPath,
		"username", bmc.Username,
		"hasPassword", bmc.Password != "",
		"requestBody", string(body))

	// Execute request
	slog.Info("Executing power control action", "bmc", bmcName, "action", action)
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute power control request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("Power control failed",
			"bmc", bmcName,
			"action", action,
			"statusCode", resp.StatusCode,
			"responseBody", string(body),
			"requestURL", targetURL)
		return fmt.Errorf("power control failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Update last seen timestamp
	if err := s.db.UpdateBMCLastSeen(ctx, bmc.ID); err != nil {
		slog.Warn("Failed to update BMC last seen", "bmc", bmcName, "error", err)
	}

	return nil
}

// TestConnection tests connectivity to a BMC
func (s *Service) TestConnection(ctx context.Context, bmc *models.BMC) error {
	targetURL, err := s.buildBMCURL(bmc, "/redfish/v1/")
	if err != nil {
		return fmt.Errorf("failed to build BMC URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create test request: %w", err)
	}

	req.SetBasicAuth(bmc.Username, bmc.Password)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to BMC: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("authentication failed")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("BMC returned error status: %d", resp.StatusCode)
	}

	return nil
}

// TestUnauthenticatedConnection tests basic connectivity to a BMC using the unauthenticated Redfish root
// This is useful for testing if the BMC is reachable and has Redfish enabled before providing credentials
func (s *Service) TestUnauthenticatedConnection(ctx context.Context, address string) error {
	// Build URL using the same logic as buildBMCURL but with just an address
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		address = "https://" + address
	}

	baseURL, err := url.Parse(address)
	if err != nil {
		return fmt.Errorf("invalid BMC address: %w", err)
	}

	targetURL := baseURL.ResolveReference(&url.URL{Path: "/redfish/v1/"})

	req, err := http.NewRequestWithContext(ctx, "GET", targetURL.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create test request: %w", err)
	}

	// Don't set authentication - test unauthenticated access
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to BMC: %w", err)
	}
	defer resp.Body.Close()

	// Accept both successful responses and 401 Unauthorized as "good" responses
	// 401 means the BMC is there and responding with Redfish, just needs auth
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized {
		return nil
	}

	// Check if this looks like a Redfish response by examining content type
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(strings.ToLower(contentType), "application/json") {
		// If it's JSON and not a server error, it's probably Redfish
		if resp.StatusCode < 500 {
			return nil
		}
	}

	return fmt.Errorf("BMC returned unexpected status: %d", resp.StatusCode)
}

// buildBMCURL constructs a URL for accessing a BMC endpoint
func (s *Service) buildBMCURL(bmc *models.BMC, path string) (string, error) {
	address := bmc.Address

	// Add https:// prefix if no protocol is specified
	// Examples:
	// - "192.168.1.100" -> "https://192.168.1.100"
	// - "bmc.example.com" -> "https://bmc.example.com"
	// - "https://192.168.1.100" -> "https://192.168.1.100" (unchanged)
	// - "http://192.168.1.100" -> "http://192.168.1.100" (unchanged)
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		address = "https://" + address
	}

	// Remove any trailing slashes from the base address
	// This ensures consistent URL construction
	address = strings.TrimRight(address, "/")

	// Ensure the Redfish path starts with /
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// Simply concatenate the base URL with the Redfish path
	// This preserves any path components in the base URL
	// Examples:
	// - "https://192.168.1.100" + "/redfish/v1/Systems" -> "https://192.168.1.100/redfish/v1/Systems"
	// - "https://mock.shoal.cloud/public-rackmount1" + "/redfish/v1/Systems" -> "https://mock.shoal.cloud/public-rackmount1/redfish/v1/Systems"
	targetURL := address + path

	// Log URL construction for debugging
	slog.Debug("Building BMC URL",
		"originalAddress", bmc.Address,
		"normalizedAddress", address,
		"path", path,
		"resultURL", targetURL)

	return targetURL, nil
}

// createProxyRequest creates an HTTP request for proxying to a BMC
func (s *Service) createProxyRequest(r *http.Request, targetURL string, bmc *models.BMC) (*http.Request, error) {
	// Copy request body if present
	var body io.Reader
	if r.Body != nil {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		body = bytes.NewReader(bodyBytes)
		// Restore original body for further processing if needed
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	// Create new request
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy request: %w", err)
	}

	// Copy headers (except Authorization which we'll replace)
	for key, values := range r.Header {
		if strings.ToLower(key) == "authorization" {
			continue // We'll set our own auth
		}
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// Set BMC authentication
	proxyReq.SetBasicAuth(bmc.Username, bmc.Password)

	return proxyReq, nil
}

// ConnectionMethod management methods for AggregationService

// AddConnectionMethod creates a new connection method and fetches initial aggregated data
func (s *Service) AddConnectionMethod(ctx context.Context, name, address, username, password string) (*models.ConnectionMethod, error) {
	// Generate a unique ID for the connection method
	id := fmt.Sprintf("cm-%d", time.Now().UnixNano())

	method := &models.ConnectionMethod{
		ID:                   id,
		Name:                 name,
		ConnectionMethodType: "Redfish",
		Address:              address,
		Username:             username,
		Password:             password,
		Enabled:              true,
	}

	// Test the connection first
	testBMC := &models.BMC{
		Address:  address,
		Username: username,
		Password: password,
	}
	if err := s.TestConnection(ctx, testBMC); err != nil {
		return nil, fmt.Errorf("failed to connect to BMC: %w", err)
	}

	// Fetch initial aggregated data
	managers, systems, err := s.FetchAggregatedData(ctx, testBMC)
	if err != nil {
		slog.Warn("Failed to fetch initial aggregated data", "error", err)
		// Continue anyway - we can fetch later
	}

	// Store the aggregated data as JSON strings
	if managers != nil {
		managersJSON, _ := json.Marshal(managers)
		method.AggregatedManagers = string(managersJSON)
	}
	if systems != nil {
		systemsJSON, _ := json.Marshal(systems)
		method.AggregatedSystems = string(systemsJSON)
	}

	// Store in database
	if err := s.db.CreateConnectionMethod(ctx, method); err != nil {
		return nil, fmt.Errorf("failed to create connection method: %w", err)
	}

	return method, nil
}

// FetchAggregatedData fetches managers and systems from a BMC
func (s *Service) FetchAggregatedData(ctx context.Context, bmc *models.BMC) ([]map[string]interface{}, []map[string]interface{}, error) {
	var managers []map[string]interface{}
	var systems []map[string]interface{}

	// Fetch managers collection
	managersURL, err := s.buildBMCURL(bmc, "/redfish/v1/Managers")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build managers URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", managersURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create managers request: %w", err)
	}
	req.SetBasicAuth(bmc.Username, bmc.Password)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch managers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var collection map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&collection); err == nil {
			// Extract member URLs and fetch each manager
			if members, ok := collection["Members"].([]interface{}); ok {
				for _, member := range members {
					if m, ok := member.(map[string]interface{}); ok {
						if odataID, ok := m["@odata.id"].(string); ok {
							// Fetch the individual manager resource
							managerData, _ := s.fetchRedfishResource(ctx, bmc, odataID)
							if managerData != nil {
								managers = append(managers, managerData)
							}
						}
					}
				}
			}
		}
	}

	// Fetch systems collection
	systemsURL, err := s.buildBMCURL(bmc, "/redfish/v1/Systems")
	if err != nil {
		return managers, nil, fmt.Errorf("failed to build systems URL: %w", err)
	}

	req, err = http.NewRequestWithContext(ctx, "GET", systemsURL, nil)
	if err != nil {
		return managers, nil, fmt.Errorf("failed to create systems request: %w", err)
	}
	req.SetBasicAuth(bmc.Username, bmc.Password)
	req.Header.Set("Accept", "application/json")

	resp, err = s.client.Do(req)
	if err != nil {
		return managers, nil, fmt.Errorf("failed to fetch systems: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var collection map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&collection); err == nil {
			// Extract member URLs and fetch each system
			if members, ok := collection["Members"].([]interface{}); ok {
				for _, member := range members {
					if m, ok := member.(map[string]interface{}); ok {
						if odataID, ok := m["@odata.id"].(string); ok {
							// Fetch the individual system resource
							systemData, _ := s.fetchRedfishResource(ctx, bmc, odataID)
							if systemData != nil {
								systems = append(systems, systemData)
							}
						}
					}
				}
			}
		}
	}

	return managers, systems, nil
}

// fetchRedfishResource fetches a single Redfish resource
func (s *Service) fetchRedfishResource(ctx context.Context, bmc *models.BMC, path string) (map[string]interface{}, error) {
	targetURL, err := s.buildBMCURL(bmc, path)
	if err != nil {
		return nil, fmt.Errorf("failed to build resource URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource request: %w", err)
	}
	req.SetBasicAuth(bmc.Username, bmc.Password)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch resource: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch resource: status %d", resp.StatusCode)
	}

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode resource: %w", err)
	}

	return data, nil
}

// RemoveConnectionMethod removes a connection method and its aggregated data
func (s *Service) RemoveConnectionMethod(ctx context.Context, id string) error {
	return s.db.DeleteConnectionMethod(ctx, id)
}

// GetConnectionMethods returns all connection methods
func (s *Service) GetConnectionMethods(ctx context.Context) ([]models.ConnectionMethod, error) {
	return s.db.GetConnectionMethods(ctx)
}

// GetConnectionMethod returns a single connection method by ID
func (s *Service) GetConnectionMethod(ctx context.Context, id string) (*models.ConnectionMethod, error) {
	return s.db.GetConnectionMethod(ctx, id)
}
