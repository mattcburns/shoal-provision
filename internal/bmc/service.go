// Shoal is a Redfish aggregator service.
// Copyright (C) 2025  Matthew Burns
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

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

// DiscoverSettings enumerates configurable settings using @Redfish.Settings and common endpoints
func (s *Service) DiscoverSettings(ctx context.Context, bmcName string, resourceFilter string) ([]models.SettingDescriptor, error) {
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

	var descriptors []models.SettingDescriptor

	// Probe Systems -> BIOS Settings
	systemID, _ := s.GetFirstSystemID(ctx, bmcName)
	if systemID != "" && (resourceFilter == "" || strings.Contains("/redfish/v1/Systems/"+systemID+"/Bios", resourceFilter)) {
		biosPath := fmt.Sprintf("/redfish/v1/Systems/%s/Bios", systemID)
		if biosData, err := s.fetchRedfishResource(ctx, bmc, biosPath); err == nil {
			// Check @Redfish.Settings
			if settingsObj := extractSettingsObject(biosData); settingsObj != "" {
				// Current values are often under Attributes or similar
				currentValues := map[string]interface{}{}
				if attrs, ok := biosData["Attributes"].(map[string]interface{}); ok {
					currentValues = attrs
				}
				descs := buildDescriptorsFromMap(bmcName, biosPath, currentValues, false, "")
				descriptors = append(descriptors, descs...)
			}
		}
	}

	// Probe Managers -> ManagerNetworkProtocol (typical writable settings like NTP, HTTP/HTTPS)
	managerID, _ := s.GetFirstManagerID(ctx, bmcName)
	if managerID != "" && (resourceFilter == "" || strings.Contains("/redfish/v1/Managers/"+managerID+"/NetworkProtocol", resourceFilter)) {
		mnpPath := fmt.Sprintf("/redfish/v1/Managers/%s/NetworkProtocol", managerID)
		if data, err := s.fetchRedfishResource(ctx, bmc, mnpPath); err == nil {
			if settingsObj := extractSettingsObject(data); settingsObj != "" {
				// Heuristically pick writable-looking fields
				current := make(map[string]interface{})
				for k, v := range data {
					// Skip metadata and complex links
					if strings.HasPrefix(k, "@") || k == "Id" || k == "Name" || k == "Description" || k == "Links" || k == "Oem" || k == "Actions" {
						continue
					}
					current[k] = v
				}
				descs := buildDescriptorsFromMap(bmcName, mnpPath, current, false, "")
				descriptors = append(descriptors, descs...)
			}
		}
	}

	// Persist discovered descriptors and values for later detail queries
	if err := s.db.UpsertSettingDescriptors(ctx, bmcName, descriptors); err != nil {
		slog.Warn("Failed to persist settings descriptors", "bmc", bmcName, "error", err)
	}
	return descriptors, nil
}

func extractSettingsObject(resource map[string]interface{}) string {
	if settings, ok := resource["@Redfish.Settings"].(map[string]interface{}); ok {
		if so, ok := settings["SettingsObject"].(map[string]interface{}); ok {
			if oid, ok := so["@odata.id"].(string); ok {
				return oid
			}
		}
	}
	return ""
}

func buildDescriptorsFromMap(bmcName, resourcePath string, values map[string]interface{}, oem bool, vendor string) []models.SettingDescriptor {
	var result []models.SettingDescriptor
	ts := time.Now().UTC().Format(time.RFC3339)
	for attr, val := range values {
		// Only leaf primitives or simple objects/arrays as-is for now
		desc := models.SettingDescriptor{
			ID:            hashID(bmcName, resourcePath, attr),
			BMCName:       bmcName,
			ResourcePath:  resourcePath,
			Attribute:     attr,
			Type:          inferType(val),
			ReadOnly:      false,
			OEM:           oem,
			OEMVendor:     vendor,
			CurrentValue:  val,
			SourceTimeISO: ts,
		}
		result = append(result, desc)
	}
	return result
}

func inferType(v interface{}) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64, float32, int, int64, json.Number:
		return "number"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		return "unknown"
	}
}

func hashID(parts ...string) string {
	// Simple deterministic ID without new dependency: join and SHA1-like hex via stdlib
	joined := strings.Join(parts, "|")
	// Use FNV-1a 64 for compactness
	var h uint64 = 1469598103934665603
	const prime uint64 = 1099511628211
	for i := 0; i < len(joined); i++ {
		h ^= uint64(joined[i])
		h *= prime
	}
	return fmt.Sprintf("%x", h)
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

// GetDetailedBMCStatus fetches comprehensive information about a BMC
func (s *Service) GetDetailedBMCStatus(ctx context.Context, bmcName string) (*models.DetailedBMCStatus, error) {
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

	status := &models.DetailedBMCStatus{
		BMC: *bmc,
	}

	// Get system information
	systemInfo, err := s.getSystemInfo(ctx, bmc)
	if err != nil {
		slog.Warn("Failed to get system info", "bmc", bmcName, "error", err)
	} else {
		status.SystemInfo = systemInfo
	}

	// Get network interfaces
	nics, err := s.getNetworkInterfaces(ctx, bmc)
	if err != nil {
		slog.Warn("Failed to get network interfaces", "bmc", bmcName, "error", err)
	} else {
		status.NetworkInterfaces = nics
	}

	// Get storage devices
	storageDevices, err := s.getStorageDevices(ctx, bmc)
	if err != nil {
		slog.Warn("Failed to get storage devices", "bmc", bmcName, "error", err)
	} else {
		status.StorageDevices = storageDevices
	}

	// Get SEL entries
	selEntries, err := s.getSELEntries(ctx, bmc)
	if err != nil {
		slog.Warn("Failed to get SEL entries", "bmc", bmcName, "error", err)
	} else {
		status.SELEntries = selEntries
	}

	// Update last seen timestamp
	if err := s.db.UpdateBMCLastSeen(ctx, bmc.ID); err != nil {
		slog.Warn("Failed to update BMC last seen", "bmc", bmcName, "error", err)
	}

	return status, nil
}

// getSystemInfo fetches system information from the first system on the BMC
func (s *Service) getSystemInfo(ctx context.Context, bmc *models.BMC) (*models.SystemInfo, error) {
	systemID, err := s.GetFirstSystemID(ctx, bmc.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get system ID: %w", err)
	}

	systemPath := fmt.Sprintf("/redfish/v1/Systems/%s", systemID)
	systemData, err := s.fetchRedfishResource(ctx, bmc, systemPath)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch system data: %w", err)
	}

	systemInfo := &models.SystemInfo{}

	// Extract system information
	if serialNumber, ok := systemData["SerialNumber"].(string); ok {
		systemInfo.SerialNumber = serialNumber
	}
	if sku, ok := systemData["SKU"].(string); ok {
		systemInfo.SKU = sku
	}
	if powerState, ok := systemData["PowerState"].(string); ok {
		systemInfo.PowerState = powerState
	}
	if model, ok := systemData["Model"].(string); ok {
		systemInfo.Model = model
	}
	if manufacturer, ok := systemData["Manufacturer"].(string); ok {
		systemInfo.Manufacturer = manufacturer
	}

	return systemInfo, nil
}

// getNetworkInterfaces fetches network interface information
func (s *Service) getNetworkInterfaces(ctx context.Context, bmc *models.BMC) ([]models.NetworkInterface, error) {
	systemID, err := s.GetFirstSystemID(ctx, bmc.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get system ID: %w", err)
	}

	// Try to get network interfaces from the system
	nicPath := fmt.Sprintf("/redfish/v1/Systems/%s/EthernetInterfaces", systemID)
	nicCollection, err := s.fetchRedfishResource(ctx, bmc, nicPath)
	if err != nil {
		// If system-level NICs aren't available, try manager-level
		managerID, err := s.GetFirstManagerID(ctx, bmc.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get manager ID: %w", err)
		}
		nicPath = fmt.Sprintf("/redfish/v1/Managers/%s/EthernetInterfaces", managerID)
		nicCollection, err = s.fetchRedfishResource(ctx, bmc, nicPath)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch network interfaces: %w", err)
		}
	}

	var nics []models.NetworkInterface

	if members, ok := nicCollection["Members"].([]interface{}); ok {
		for _, member := range members {
			if memberMap, ok := member.(map[string]interface{}); ok {
				if odataID, ok := memberMap["@odata.id"].(string); ok {
					// Fetch detailed NIC information
					nicData, err := s.fetchRedfishResource(ctx, bmc, odataID)
					if err != nil {
						slog.Warn("Failed to fetch NIC details", "path", odataID, "error", err)
						continue
					}

					nic := models.NetworkInterface{}
					if name, ok := nicData["Name"].(string); ok {
						nic.Name = name
					}
					if description, ok := nicData["Description"].(string); ok {
						nic.Description = description
					}
					if macAddress, ok := nicData["MACAddress"].(string); ok {
						nic.MACAddress = macAddress
					}

					// Try to get IP addresses from IPv4Addresses
					if ipv4Addresses, ok := nicData["IPv4Addresses"].([]interface{}); ok {
						for _, addr := range ipv4Addresses {
							if addrMap, ok := addr.(map[string]interface{}); ok {
								if address, ok := addrMap["Address"].(string); ok && address != "" {
									nic.IPAddresses = append(nic.IPAddresses, address)
								}
							}
						}
					}

					nics = append(nics, nic)
				}
			}
		}
	}

	return nics, nil
}

// getStorageDevices fetches storage device information from both Storage and SimpleStorage endpoints
func (s *Service) getStorageDevices(ctx context.Context, bmc *models.BMC) ([]models.StorageDevice, error) {
	systemID, err := s.GetFirstSystemID(ctx, bmc.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get system ID: %w", err)
	}

	var storageDevices []models.StorageDevice

	// First, try to get devices from regular Storage collection
	storageDevices = append(storageDevices, s.getStorageDevicesFromStorage(ctx, bmc, systemID)...)

	// Then, try to get devices from SimpleStorage collection
	storageDevices = append(storageDevices, s.getStorageDevicesFromSimpleStorage(ctx, bmc, systemID)...)

	// Return the (possibly empty) list of storage devices
	return storageDevices, nil
}

// getStorageDevicesFromStorage fetches storage devices from the regular Storage collection
func (s *Service) getStorageDevicesFromStorage(ctx context.Context, bmc *models.BMC, systemID string) []models.StorageDevice {
	// Get storage collection
	storagePath := fmt.Sprintf("/redfish/v1/Systems/%s/Storage", systemID)
	storageCollection, err := s.fetchRedfishResource(ctx, bmc, storagePath)
	if err != nil {
		slog.Debug("Storage collection not available", "path", storagePath, "error", err)
		return nil
	}

	var storageDevices []models.StorageDevice

	if members, ok := storageCollection["Members"].([]interface{}); ok {
		for _, member := range members {
			if memberMap, ok := member.(map[string]interface{}); ok {
				if odataID, ok := memberMap["@odata.id"].(string); ok {
					// Fetch storage controller details
					storageData, err := s.fetchRedfishResource(ctx, bmc, odataID)
					if err != nil {
						slog.Warn("Failed to fetch storage details", "path", odataID, "error", err)
						continue
					}

					// Get drives from this storage controller
					if drives, ok := storageData["Drives"].([]interface{}); ok {
						for _, drive := range drives {
							if driveRef, ok := drive.(map[string]interface{}); ok {
								if driveODataID, ok := driveRef["@odata.id"].(string); ok {
									// Fetch drive details
									driveData, err := s.fetchRedfishResource(ctx, bmc, driveODataID)
									if err != nil {
										slog.Warn("Failed to fetch drive details", "path", driveODataID, "error", err)
										continue
									}

									device := s.parseStorageDevice(driveData)
									storageDevices = append(storageDevices, device)
								}
							}
						}
					}
				}
			}
		}
	}

	return storageDevices
}

// getStorageDevicesFromSimpleStorage fetches storage devices from the SimpleStorage collection
func (s *Service) getStorageDevicesFromSimpleStorage(ctx context.Context, bmc *models.BMC, systemID string) []models.StorageDevice {
	// Get SimpleStorage collection
	simpleStoragePath := fmt.Sprintf("/redfish/v1/Systems/%s/SimpleStorage", systemID)
	simpleStorageCollection, err := s.fetchRedfishResource(ctx, bmc, simpleStoragePath)
	if err != nil {
		slog.Debug("SimpleStorage collection not available", "path", simpleStoragePath, "error", err)
		return nil
	}

	var storageDevices []models.StorageDevice

	if members, ok := simpleStorageCollection["Members"].([]interface{}); ok {
		for _, member := range members {
			if memberMap, ok := member.(map[string]interface{}); ok {
				if odataID, ok := memberMap["@odata.id"].(string); ok {
					// Fetch SimpleStorage controller details
					simpleStorageData, err := s.fetchRedfishResource(ctx, bmc, odataID)
					if err != nil {
						slog.Warn("Failed to fetch SimpleStorage details", "path", odataID, "error", err)
						continue
					}

					// Get devices directly embedded in SimpleStorage controller
					if devices, ok := simpleStorageData["Devices"].([]interface{}); ok {
						for _, device := range devices {
							if deviceMap, ok := device.(map[string]interface{}); ok {
								// Skip devices that are not present/enabled
								if status, ok := deviceMap["Status"].(map[string]interface{}); ok {
									if state, ok := status["State"].(string); ok && state == "Absent" {
										continue
									}
								}

								storageDevice := s.parseStorageDevice(deviceMap)
								storageDevices = append(storageDevices, storageDevice)
							}
						}
					}
				}
			}
		}
	}

	return storageDevices
}

// parseStorageDevice parses storage device data from either Storage Drive or SimpleStorage Device format
func (s *Service) parseStorageDevice(deviceData map[string]interface{}) models.StorageDevice {
	device := models.StorageDevice{}

	if name, ok := deviceData["Name"].(string); ok {
		device.Name = name
	}
	if model, ok := deviceData["Model"].(string); ok {
		device.Model = model
	}
	if serialNumber, ok := deviceData["SerialNumber"].(string); ok {
		device.SerialNumber = serialNumber
	}
	if capacityBytes, ok := deviceData["CapacityBytes"].(float64); ok {
		device.CapacityBytes = int64(capacityBytes)
	}

	// Handle status - can be either a string (older format) or an object with Health property
	if status, ok := deviceData["Status"].(map[string]interface{}); ok {
		if health, ok := status["Health"].(string); ok {
			device.Status = health
		}
	} else if statusStr, ok := deviceData["Status"].(string); ok {
		device.Status = statusStr
	}

	if mediaType, ok := deviceData["MediaType"].(string); ok {
		device.MediaType = mediaType
	}

	// For SimpleStorage devices, also check for Manufacturer field
	if manufacturer, ok := deviceData["Manufacturer"].(string); ok {
		// Add manufacturer info to the model field if available
		if device.Model != "" {
			device.Model = fmt.Sprintf("%s %s", manufacturer, device.Model)
		} else {
			device.Model = manufacturer
		}
	}

	return device
}

// getSELEntries fetches System Event Log entries
func (s *Service) getSELEntries(ctx context.Context, bmc *models.BMC) ([]models.SELEntry, error) {
	managerID, err := s.GetFirstManagerID(ctx, bmc.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get manager ID: %w", err)
	}

	// Try to get log services
	logServicesPath := fmt.Sprintf("/redfish/v1/Managers/%s/LogServices", managerID)
	logServicesData, err := s.fetchRedfishResource(ctx, bmc, logServicesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch log services: %w", err)
	}

	var selEntries []models.SELEntry

	// Look for the Event log or SEL log
	if members, ok := logServicesData["Members"].([]interface{}); ok {
		for _, member := range members {
			if memberMap, ok := member.(map[string]interface{}); ok {
				if odataID, ok := memberMap["@odata.id"].(string); ok {
					// Get log service details to find the right one
					logServiceData, err := s.fetchRedfishResource(ctx, bmc, odataID)
					if err != nil {
						continue
					}

					// Look for Event log or SEL
					if name, ok := logServiceData["Name"].(string); ok {
						if name == "Event Log" || name == "SEL Log" || name == "System Event Log" {
							// Found the right log service, get entries
							entriesPath := odataID + "/Entries"
							entriesData, err := s.fetchRedfishResource(ctx, bmc, entriesPath)
							if err != nil {
								continue
							}

							if entryMembers, ok := entriesData["Members"].([]interface{}); ok {
								for _, entryMember := range entryMembers {
									if entryRef, ok := entryMember.(map[string]interface{}); ok {
										if entryODataID, ok := entryRef["@odata.id"].(string); ok {
											// Fetch individual entry
											entryData, err := s.fetchRedfishResource(ctx, bmc, entryODataID)
											if err != nil {
												continue
											}

											entry := models.SELEntry{}
											if id, ok := entryData["Id"].(string); ok {
												entry.ID = id
											}
											if message, ok := entryData["Message"].(string); ok {
												entry.Message = message
											}
											if severity, ok := entryData["Severity"].(string); ok {
												entry.Severity = severity
											}
											if created, ok := entryData["Created"].(string); ok {
												entry.Created = created
											}
											if entryType, ok := entryData["EntryType"].(string); ok {
												entry.EntryType = entryType
											}

											selEntries = append(selEntries, entry)
										}
									}
								}
							}
							break // Found the right log service
						}
					}
				}
			}
		}
	}

	return selEntries, nil
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
