/*
Shoal is a Redfish aggregator service.
Copyright (C) 2025  Matthew Burns

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"shoal/pkg/redfish"
)

// handleSessionService routes and implements SessionService endpoints.
// Extracted from api.go per design 019. Behavior is unchanged.
func (h *Handler) handleSessionService(w http.ResponseWriter, r *http.Request, path string) {
	// Remove /v1/SessionService prefix
	subPath := strings.TrimPrefix(path, "/v1/SessionService")

	// OPTIONS handling (allow unauthenticated for CORS-style discovery)
	if subPath == "" || subPath == "/" {
		if r.Method == http.MethodOptions {
			h.writeAllow(w, http.MethodGet)
			return
		}
	} else if subPath == "/Sessions" || subPath == "/Sessions/" {
		if r.Method == http.MethodOptions {
			h.writeAllow(w, http.MethodGet, http.MethodPost)
			return
		}
	} else if strings.HasPrefix(subPath, "/Sessions/") {
		if r.Method == http.MethodOptions {
			h.writeAllow(w, http.MethodGet, http.MethodDelete)
			return
		}
	}

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

// handleGetSessionServiceRoot returns the SessionService root resource.
// Extracted from api.go per design 019. Behavior is unchanged.
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

// handleGetSessionsCollection lists active sessions.
// Extracted from api.go per design 019. Behavior is unchanged.
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

// handleGetSession returns an individual session resource.
// Extracted from api.go per design 019. Behavior is unchanged.
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

// handleDeleteSession deletes a session by ID (logout).
// Extracted from api.go per design 019. Behavior is unchanged.
func (h *Handler) handleDeleteSession(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.db.DeleteSessionByID(r.Context(), id); err != nil {
		slog.Error("Failed to delete session", "id", id, "error", err)
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to delete session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
