package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"shoal/internal/auth"
	"shoal/internal/bmc"
	"shoal/internal/database"
	"shoal/pkg/models"
	"shoal/pkg/redfish"
)

// Handler implements the Redfish API endpoints
type Handler struct {
	db     *database.DB
	auth   *auth.Authenticator
	bmcSvc *bmc.Service
}

// New creates a new API handler
func New(db *database.DB) http.Handler {
	h := &Handler{
		db:     db,
		auth:   auth.New(db),
		bmcSvc: bmc.New(db),
	}

	mux := http.NewServeMux()

	// Redfish service root
	mux.HandleFunc("/redfish/", h.handleRedfish)

	// BMC management endpoints (aggregator-specific)
	mux.HandleFunc("/redfish/v1/AggregationService/ManagedNodes/", h.auth.RequireAuth(http.HandlerFunc(h.handleManagedNodes)).ServeHTTP)

	return mux
}

// handleRedfish routes Redfish API requests
func (h *Handler) handleRedfish(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/redfish")

	slog.Debug("Handling Redfish request", "method", r.Method, "path", path)

	// Handle service root
	if path == "/" || path == "" {
		h.handleServiceRoot(w, r)
		return
	}

	// Handle version endpoint
	if path == "/v1/" || path == "/v1" {
		h.handleServiceRoot(w, r)
		return
	}

	// Handle authentication endpoints (no auth required)
	if (path == "/v1/SessionService/Sessions" || path == "/v1/SessionService/Sessions/") && r.Method == "POST" {
		h.handleLogin(w, r)
		return
	}

	// All other endpoints require authentication
	user, err := h.auth.AuthenticateRequest(r)
	if err != nil {
		h.writeErrorResponse(w, http.StatusUnauthorized, "Base.1.0.Unauthorized", "Authentication required")
		return
	}

	// Check if this is a BMC proxy request
	if h.isBMCProxyRequest(path) {
		h.handleBMCProxy(w, r, path)
		return
	}

	// Handle aggregator-specific endpoints
	h.handleAggregatorEndpoints(w, r, path, user)
}

// handleServiceRoot returns the Redfish service root
func (h *Handler) handleServiceRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
		return
	}

	serviceRoot := redfish.ServiceRoot{
		ODataContext:   "/redfish/v1/$metadata#ServiceRoot.ServiceRoot",
		ODataID:        "/redfish/v1/",
		ODataType:      "#ServiceRoot.v1_5_0.ServiceRoot",
		ID:             "RootService",
		Name:           "Shoal Redfish Aggregator",
		RedfishVersion: "1.6.0",
		UUID:           "12345678-1234-1234-1234-123456789012", // TODO: Generate proper UUID
		Links: redfish.ServiceRootLinks{
			Sessions: redfish.ODataIDRef{ODataID: "/redfish/v1/SessionService/Sessions"},
		},
		Systems:            redfish.ODataIDRef{ODataID: "/redfish/v1/Systems"},
		Managers:           redfish.ODataIDRef{ODataID: "/redfish/v1/Managers"},
		SessionService:     redfish.ODataIDRef{ODataID: "/redfish/v1/SessionService"},
		AggregationService: &redfish.ODataIDRef{ODataID: "/redfish/v1/AggregationService"},
	}

	h.writeJSONResponse(w, http.StatusOK, serviceRoot)
}

// handleLogin creates a new session
func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
		return
	}

	// Parse credentials from request body
	var loginReq struct {
		Username string `json:"UserName"`
		Password string `json:"Password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&loginReq); err != nil {
		h.writeErrorResponse(w, http.StatusBadRequest, "Base.1.0.MalformedJSON", "Invalid JSON in request body")
		return
	}

	// Validate credentials
	user, err := h.auth.AuthenticateBasic(r.Context(), loginReq.Username, loginReq.Password)
	if err != nil {
		h.writeErrorResponse(w, http.StatusUnauthorized, "Base.1.0.Unauthorized", "Invalid credentials")
		return
	}

	// Create session
	session, err := h.auth.CreateSession(r.Context(), user.ID)
	if err != nil {
		slog.Error("Failed to create session", "error", err)
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to create session")
		return
	}

	// Return session info
	sessionResp := map[string]interface{}{
		"@odata.context": "/redfish/v1/$metadata#Session.Session",
		"@odata.id":      fmt.Sprintf("/redfish/v1/SessionService/Sessions/%s", session.ID),
		"@odata.type":    "#Session.v1_0_0.Session",
		"Id":             session.ID,
		"Name":           "User Session",
		"UserName":       user.Username,
	}

	// Set session token header
	w.Header().Set("X-Auth-Token", session.Token)
	w.Header().Set("Location", fmt.Sprintf("/redfish/v1/SessionService/Sessions/%s", session.ID))
	h.writeJSONResponse(w, http.StatusCreated, sessionResp)
}

// handleBMCProxy proxies requests to managed BMCs
func (h *Handler) handleBMCProxy(w http.ResponseWriter, r *http.Request, path string) {
	// Extract BMC name from path (e.g., /v1/Managers/bmc1/...)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 {
		h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Resource not found")
		return
	}

	var bmcName string
	var bmcPath string

	// Handle different proxy patterns
	if parts[1] == "Managers" && len(parts) >= 3 {
		bmcName = parts[2]
		// Get the actual manager ID from the BMC
		managerID, err := h.bmcSvc.GetFirstManagerID(r.Context(), bmcName)
		if err != nil {
			slog.Error("Failed to get manager ID", "bmc", bmcName, "error", err)
			// Fall back to common defaults
			managerID = "1" // Common default
		}

		if len(parts) == 3 {
			// Request for the manager root
			bmcPath = fmt.Sprintf("/redfish/v1/Managers/%s", managerID)
		} else {
			// Sub-resource request
			bmcPath = fmt.Sprintf("/redfish/v1/Managers/%s/%s", managerID, strings.Join(parts[3:], "/"))
		}
	} else if parts[1] == "Systems" && len(parts) >= 3 {
		// Extract BMC name and system path
		bmcName = parts[2]
		// Get the actual system ID from the BMC
		systemID, err := h.bmcSvc.GetFirstSystemID(r.Context(), bmcName)
		if err != nil {
			slog.Error("Failed to get system ID", "bmc", bmcName, "error", err)
			// Fall back to common defaults
			systemID = "1" // Common default
		}

		if len(parts) == 3 {
			// Request for the system root
			bmcPath = fmt.Sprintf("/redfish/v1/Systems/%s", systemID)
		} else {
			// Sub-resource request
			bmcPath = fmt.Sprintf("/redfish/v1/Systems/%s/%s", systemID, strings.Join(parts[3:], "/"))
		}
	} else {
		h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Resource not found")
		return
	}

	// Proxy request to BMC
	resp, err := h.bmcSvc.ProxyRequest(r.Context(), bmcName, bmcPath, r)
	if err != nil {
		slog.Error("Failed to proxy request to BMC", "bmc", bmcName, "path", bmcPath, "error", err)
		h.writeErrorResponse(w, http.StatusBadGateway, "Base.1.0.InternalError", fmt.Sprintf("Failed to communicate with BMC: %v", err))
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Copy status code and body
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// isBMCProxyRequest checks if the request should be proxied to a BMC
func (h *Handler) isBMCProxyRequest(path string) bool {
	// Proxy requests for Managers and Systems endpoints with BMC names
	managerPattern := regexp.MustCompile(`^/v1/Managers/[^/]+(/.*)?$`)
	systemPattern := regexp.MustCompile(`^/v1/Systems/[^/]+(/.*)?$`)

	return managerPattern.MatchString(path) || systemPattern.MatchString(path)
}

// handleAggregatorEndpoints handles aggregator-specific endpoints
func (h *Handler) handleAggregatorEndpoints(w http.ResponseWriter, r *http.Request, path string, user *models.User) {
	if strings.HasPrefix(path, "/v1/AggregationService") {
		h.handleAggregationService(w, r, path)
		return
	}

	if path == "/v1/Managers" || path == "/v1/Managers/" {
		h.handleManagersCollection(w, r)
		return
	}

	if path == "/v1/Systems" || path == "/v1/Systems/" {
		h.handleSystemsCollection(w, r)
		return
	}

	h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Resource not found")
}

// handleManagersCollection returns the list of managed BMCs as managers
func (h *Handler) handleManagersCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
		return
	}

	bmcs, err := h.db.GetBMCs(r.Context())
	if err != nil {
		slog.Error("Failed to get BMCs", "error", err)
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to retrieve BMCs")
		return
	}

	var members []redfish.ODataIDRef
	for _, bmc := range bmcs {
		if bmc.Enabled {
			members = append(members, redfish.ODataIDRef{
				ODataID: fmt.Sprintf("/redfish/v1/Managers/%s", bmc.Name),
			})
		}
	}

	collection := redfish.Collection{
		ODataContext: "/redfish/v1/$metadata#ManagerCollection.ManagerCollection",
		ODataID:      "/redfish/v1/Managers",
		ODataType:    "#ManagerCollection.ManagerCollection",
		Name:         "Manager Collection",
		Members:      members,
		MembersCount: len(members),
	}

	h.writeJSONResponse(w, http.StatusOK, collection)
}

// handleSystemsCollection returns the list of systems from managed BMCs
func (h *Handler) handleSystemsCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
		return
	}

	bmcs, err := h.db.GetBMCs(r.Context())
	if err != nil {
		slog.Error("Failed to get BMCs", "error", err)
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to retrieve BMCs")
		return
	}

	var members []redfish.ODataIDRef
	for _, bmc := range bmcs {
		if bmc.Enabled {
			members = append(members, redfish.ODataIDRef{
				ODataID: fmt.Sprintf("/redfish/v1/Systems/%s", bmc.Name),
			})
		}
	}

	collection := redfish.Collection{
		ODataContext: "/redfish/v1/$metadata#ComputerSystemCollection.ComputerSystemCollection",
		ODataID:      "/redfish/v1/Systems",
		ODataType:    "#ComputerSystemCollection.ComputerSystemCollection",
		Name:         "Computer System Collection",
		Members:      members,
		MembersCount: len(members),
	}

	h.writeJSONResponse(w, http.StatusOK, collection)
}

// writeJSONResponse writes a JSON response
func (h *Handler) writeJSONResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("Failed to encode JSON response", "error", err)
	}
}

// handleSessions handles session-related operations
func (h *Handler) handleSessions(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement session management endpoints
	h.writeErrorResponse(w, http.StatusNotImplemented, "Base.1.0.NotImplemented", "Sessions endpoint not yet implemented")
}

// handleManagedNodes handles BMC management operations
func (h *Handler) handleManagedNodes(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement BMC management endpoints
	h.writeErrorResponse(w, http.StatusNotImplemented, "Base.1.0.NotImplemented", "Managed nodes endpoint not yet implemented")
}

// handleAggregationService handles aggregation service endpoints
func (h *Handler) handleAggregationService(w http.ResponseWriter, r *http.Request, path string) {
	// TODO: Implement aggregation service endpoints
	h.writeErrorResponse(w, http.StatusNotImplemented, "Base.1.0.NotImplemented", "Aggregation service not yet implemented")
}

// writeErrorResponse writes a Redfish-compliant error response
func (h *Handler) writeErrorResponse(w http.ResponseWriter, status int, code, message string) {
	// Set WWW-Authenticate header for 401 responses
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Basic realm="Redfish"`)
	}

	errorResp := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}

	h.writeJSONResponse(w, status, errorResp)
}
