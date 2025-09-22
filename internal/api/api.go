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

package api

import (
	"context"
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

	// SessionService endpoints (handle auth within the handler to allow unauthenticated POST)
	if strings.HasPrefix(path, "/v1/SessionService") {
		h.handleSessionService(w, r, path)
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

	// Handle AggregationService endpoints
	if strings.HasPrefix(path, "/v1/AggregationService") {
		h.handleAggregationService(w, r, path, user)
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

	// Ensure authenticated user is present in context so audits can include it
	user, _ := h.auth.AuthenticateRequest(r)
	ctx := r.Context()
	if user != nil {
		ctx = context.WithValue(ctx, "user", user)
	}
	// Proxy request to BMC
	resp, err := h.bmcSvc.ProxyRequest(ctx, bmcName, bmcPath, r)
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

	// Get both regular BMCs and ConnectionMethods
	bmcs, err := h.db.GetBMCs(r.Context())
	if err != nil {
		slog.Error("Failed to get BMCs", "error", err)
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to retrieve BMCs")
		return
	}

	methods, err := h.bmcSvc.GetConnectionMethods(r.Context())
	if err != nil {
		slog.Error("Failed to get connection methods", "error", err)
		// Don't fail entirely if we can't get connection methods
	}

	var members []redfish.ODataIDRef

	// Add regular BMCs
	for _, bmc := range bmcs {
		if bmc.Enabled {
			members = append(members, redfish.ODataIDRef{
				ODataID: fmt.Sprintf("/redfish/v1/Managers/%s", bmc.Name),
			})
		}
	}

	// Add aggregated managers from ConnectionMethods
	for _, method := range methods {
		if method.Enabled && method.AggregatedManagers != "" {
			// Parse the aggregated managers JSON
			var managers []map[string]interface{}
			if err := json.Unmarshal([]byte(method.AggregatedManagers), &managers); err == nil {
				for _, manager := range managers {
					if odataID, ok := manager["@odata.id"].(string); ok {
						// Prefix with the connection method ID to make it unique
						modifiedID := fmt.Sprintf("/redfish/v1/Managers/%s%s", method.ID, odataID)
						members = append(members, redfish.ODataIDRef{
							ODataID: modifiedID,
						})
					}
				}
			}
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

	// Get both regular BMCs and ConnectionMethods
	bmcs, err := h.db.GetBMCs(r.Context())
	if err != nil {
		slog.Error("Failed to get BMCs", "error", err)
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to retrieve BMCs")
		return
	}

	methods, err := h.bmcSvc.GetConnectionMethods(r.Context())
	if err != nil {
		slog.Error("Failed to get connection methods", "error", err)
		// Don't fail entirely if we can't get connection methods
	}

	var members []redfish.ODataIDRef

	// Add regular BMCs
	for _, bmc := range bmcs {
		if bmc.Enabled {
			members = append(members, redfish.ODataIDRef{
				ODataID: fmt.Sprintf("/redfish/v1/Systems/%s", bmc.Name),
			})
		}
	}

	// Add aggregated systems from ConnectionMethods
	for _, method := range methods {
		if method.Enabled && method.AggregatedSystems != "" {
			// Parse the aggregated systems JSON
			var systems []map[string]interface{}
			if err := json.Unmarshal([]byte(method.AggregatedSystems), &systems); err == nil {
				for _, system := range systems {
					if odataID, ok := system["@odata.id"].(string); ok {
						// Prefix with the connection method ID to make it unique
						modifiedID := fmt.Sprintf("/redfish/v1/Systems/%s%s", method.ID, odataID)
						members = append(members, redfish.ODataIDRef{
							ODataID: modifiedID,
						})
					}
				}
			}
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

// handleSessions handles session-related operations (deprecated, replaced by handleSessionService)
func (h *Handler) handleSessions(w http.ResponseWriter, r *http.Request) {
	h.writeErrorResponse(w, http.StatusNotImplemented, "Base.1.0.NotImplemented", "Sessions endpoint not implemented here")
}

// handleManagedNodes handles BMC management operations
func (h *Handler) handleManagedNodes(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement BMC management endpoints
	h.writeErrorResponse(w, http.StatusNotImplemented, "Base.1.0.NotImplemented", "Managed nodes endpoint not yet implemented")
}

// handleAggregationService handles aggregation service endpoints
func (h *Handler) handleAggregationService(w http.ResponseWriter, r *http.Request, path string, user *models.User) {
	// Remove /v1/AggregationService prefix
	subPath := strings.TrimPrefix(path, "/v1/AggregationService")

	if subPath == "" || subPath == "/" {
		// Handle AggregationService root
		if r.Method != "GET" {
			h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
			return
		}

		aggService := redfish.AggregationService{
			ODataContext: "/redfish/v1/$metadata#AggregationService.AggregationService",
			ODataID:      "/redfish/v1/AggregationService",
			ODataType:    "#AggregationService.v1_0_0.AggregationService",
			ID:           "AggregationService",
			Name:         "Aggregation Service",
			Description:  "BMC Aggregation Service",
			ConnectionMethods: redfish.ODataIDRef{
				ODataID: "/redfish/v1/AggregationService/ConnectionMethods",
			},
		}

		h.writeJSONResponse(w, http.StatusOK, aggService)
		return
	}

	if subPath == "/ConnectionMethods" || subPath == "/ConnectionMethods/" {
		// Handle ConnectionMethods collection
		h.handleConnectionMethodsCollection(w, r, user)
		return
	}

	if strings.HasPrefix(subPath, "/ConnectionMethods/") {
		// Handle individual ConnectionMethod
		parts := strings.Split(strings.TrimPrefix(subPath, "/ConnectionMethods/"), "/")
		if len(parts) == 1 && parts[0] != "" {
			h.handleConnectionMethod(w, r, parts[0], user)
			return
		}
	}

	h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Resource not found")
}

// handleConnectionMethodsCollection handles the ConnectionMethods collection
func (h *Handler) handleConnectionMethodsCollection(w http.ResponseWriter, r *http.Request, user *models.User) {
	switch r.Method {
	case "GET":
		// Get all connection methods
		methods, err := h.bmcSvc.GetConnectionMethods(r.Context())
		if err != nil {
			slog.Error("Failed to get connection methods", "error", err)
			h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to retrieve connection methods")
			return
		}

		var members []redfish.ODataIDRef
		for _, method := range methods {
			members = append(members, redfish.ODataIDRef{
				ODataID: fmt.Sprintf("/redfish/v1/AggregationService/ConnectionMethods/%s", method.ID),
			})
		}

		collection := redfish.Collection{
			ODataContext: "/redfish/v1/$metadata#ConnectionMethodCollection.ConnectionMethodCollection",
			ODataID:      "/redfish/v1/AggregationService/ConnectionMethods",
			ODataType:    "#ConnectionMethodCollection.ConnectionMethodCollection",
			Name:         "Connection Method Collection",
			Members:      members,
			MembersCount: len(members),
		}

		h.writeJSONResponse(w, http.StatusOK, collection)

	case "POST":
		// Create a new connection method
		var req struct {
			Name                 string `json:"Name"`
			ConnectionMethodType string `json:"ConnectionMethodType"`
			Address              string `json:"ConnectionMethodVariant.Address"`
			Authentication       struct {
				Username string `json:"Username"`
				Password string `json:"Password"`
			} `json:"ConnectionMethodVariant.Authentication"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeErrorResponse(w, http.StatusBadRequest, "Base.1.0.MalformedJSON", "Invalid JSON in request body")
			return
		}

		// Validate required fields
		if req.Name == "" || req.Address == "" || req.Authentication.Username == "" || req.Authentication.Password == "" {
			h.writeErrorResponse(w, http.StatusBadRequest, "Base.1.0.PropertyMissing", "Required properties missing")
			return
		}

		// Default to Redfish if not specified
		if req.ConnectionMethodType == "" {
			req.ConnectionMethodType = "Redfish"
		}

		// Create the connection method
		method, err := h.bmcSvc.AddConnectionMethod(
			r.Context(),
			req.Name,
			req.Address,
			req.Authentication.Username,
			req.Authentication.Password,
		)
		if err != nil {
			slog.Error("Failed to create connection method", "error", err)
			if strings.Contains(err.Error(), "failed to connect") {
				h.writeErrorResponse(w, http.StatusBadRequest, "Base.1.0.ResourceCannotBeCreated", "Unable to connect to BMC")
			} else {
				h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to create connection method")
			}
			return
		}

		// Return the created connection method
		connMethod := redfish.ConnectionMethod{
			ODataContext:         "/redfish/v1/$metadata#ConnectionMethod.ConnectionMethod",
			ODataID:              fmt.Sprintf("/redfish/v1/AggregationService/ConnectionMethods/%s", method.ID),
			ODataType:            "#ConnectionMethod.v1_0_0.ConnectionMethod",
			ID:                   method.ID,
			Name:                 method.Name,
			ConnectionMethodType: method.ConnectionMethodType,
			ConnectionMethodVariant: redfish.ConnectionMethodVariant{
				ODataType: "#ConnectionMethod.v1_0_0.ConnectionMethodVariant",
				Address:   method.Address,
			},
		}

		w.Header().Set("Location", fmt.Sprintf("/redfish/v1/AggregationService/ConnectionMethods/%s", method.ID))
		h.writeJSONResponse(w, http.StatusCreated, connMethod)

	default:
		h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
	}
}

// handleConnectionMethod handles a specific ConnectionMethod resource
func (h *Handler) handleConnectionMethod(w http.ResponseWriter, r *http.Request, id string, user *models.User) {
	switch r.Method {
	case "GET":
		method, err := h.bmcSvc.GetConnectionMethod(r.Context(), id)
		if err != nil {
			slog.Error("Failed to get connection method", "id", id, "error", err)
			h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to retrieve connection method")
			return
		}
		if method == nil {
			h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Connection method not found")
			return
		}

		connMethod := redfish.ConnectionMethod{
			ODataContext:         "/redfish/v1/$metadata#ConnectionMethod.ConnectionMethod",
			ODataID:              fmt.Sprintf("/redfish/v1/AggregationService/ConnectionMethods/%s", method.ID),
			ODataType:            "#ConnectionMethod.v1_0_0.ConnectionMethod",
			ID:                   method.ID,
			Name:                 method.Name,
			ConnectionMethodType: method.ConnectionMethodType,
			ConnectionMethodVariant: redfish.ConnectionMethodVariant{
				ODataType: "#ConnectionMethod.v1_0_0.ConnectionMethodVariant",
				Address:   method.Address,
			},
		}

		h.writeJSONResponse(w, http.StatusOK, connMethod)

	case "DELETE":
		err := h.bmcSvc.RemoveConnectionMethod(r.Context(), id)
		if err != nil {
			slog.Error("Failed to delete connection method", "id", id, "error", err)
			h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to delete connection method")
			return
		}

		w.WriteHeader(http.StatusNoContent)

	default:
		h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
	}
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

// handleSessionService routes and implements SessionService endpoints
func (h *Handler) handleSessionService(w http.ResponseWriter, r *http.Request, path string) {
	// Remove /v1/SessionService prefix
	subPath := strings.TrimPrefix(path, "/v1/SessionService")

	// Allow unauthenticated session creation
	if (subPath == "/Sessions" || subPath == "/Sessions/") && r.Method == http.MethodPost {
		h.handleLogin(w, r)
		return
	}

	// All other SessionService endpoints require authentication
	if _, err := h.auth.AuthenticateRequest(r); err != nil {
		h.writeErrorResponse(w, http.StatusUnauthorized, "Base.1.0.Unauthorized", "Authentication required")
		return
	}

	// SessionService root
	if subPath == "" || subPath == "/" {
		if r.Method != http.MethodGet {
			h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
			return
		}
		h.handleGetSessionServiceRoot(w, r)
		return
	}

	// Sessions collection
	if subPath == "/Sessions" || subPath == "/Sessions/" {
		if r.Method == http.MethodGet {
			h.handleGetSessionsCollection(w, r)
			return
		}
		h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
		return
	}

	// Individual session resource
	if strings.HasPrefix(subPath, "/Sessions/") {
		id := strings.TrimPrefix(subPath, "/Sessions/")
		id = strings.Trim(id, "/")
		if id == "" {
			h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Session not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			h.handleGetSession(w, r, id)
			return
		case http.MethodDelete:
			h.handleDeleteSession(w, r, id)
			return
		default:
			h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
			return
		}
	}

	h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Resource not found")
}

// handleGetSessionServiceRoot returns the SessionService root resource
func (h *Handler) handleGetSessionServiceRoot(w http.ResponseWriter, r *http.Request) {
	service := redfish.SessionService{
		ODataContext:   "/redfish/v1/$metadata#SessionService.SessionService",
		ODataID:        "/redfish/v1/SessionService",
		ODataType:      "#SessionService.v1_0_0.SessionService",
		ID:             "SessionService",
		Name:           "Session Service",
		Description:    "User Session Service",
		ServiceEnabled: true,
		SessionTimeout: 1800,
		Sessions:       redfish.ODataIDRef{ODataID: "/redfish/v1/SessionService/Sessions"},
	}
	h.writeJSONResponse(w, http.StatusOK, service)
}

// handleGetSessionsCollection lists active sessions
func (h *Handler) handleGetSessionsCollection(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.db.GetSessions(r.Context())
	if err != nil {
		slog.Error("Failed to get sessions", "error", err)
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to retrieve sessions")
		return
	}

	var members []redfish.ODataIDRef
	for _, s := range sessions {
		members = append(members, redfish.ODataIDRef{ODataID: fmt.Sprintf("/redfish/v1/SessionService/Sessions/%s", s.ID)})
	}

	collection := redfish.Collection{
		ODataContext: "/redfish/v1/$metadata#SessionCollection.SessionCollection",
		ODataID:      "/redfish/v1/SessionService/Sessions",
		ODataType:    "#SessionCollection.SessionCollection",
		Name:         "Session Collection",
		Members:      members,
		MembersCount: len(members),
	}
	h.writeJSONResponse(w, http.StatusOK, collection)
}

// handleGetSession returns an individual session resource
func (h *Handler) handleGetSession(w http.ResponseWriter, r *http.Request, id string) {
	s, err := h.db.GetSession(r.Context(), id)
	if err != nil {
		slog.Error("Failed to get session", "id", id, "error", err)
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to retrieve session")
		return
	}
	if s == nil {
		h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Session not found")
		return
	}

	user, err := h.db.GetUser(r.Context(), s.UserID)
	if err != nil || user == nil {
		// Don't reveal details
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to retrieve session user")
		return
	}

	resp := redfish.Session{
		ODataContext: "/redfish/v1/$metadata#Session.Session",
		ODataID:      fmt.Sprintf("/redfish/v1/SessionService/Sessions/%s", s.ID),
		ODataType:    "#Session.v1_0_0.Session",
		ID:           s.ID,
		Name:         "User Session",
		UserName:     user.Username,
	}
	h.writeJSONResponse(w, http.StatusOK, resp)
}

// handleDeleteSession deletes a session by ID (logout)
func (h *Handler) handleDeleteSession(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.db.DeleteSessionByID(r.Context(), id); err != nil {
		slog.Error("Failed to delete session", "id", id, "error", err)
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to delete session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
