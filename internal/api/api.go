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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"shoal/internal/auth"
	"shoal/internal/bmc"
	"shoal/internal/ctxkeys"
	"shoal/internal/database"
	"shoal/pkg/models"
	"shoal/pkg/redfish"
)

// validMessageIDs contains the set of valid Base message registry IDs
var validMessageIDs = map[string]struct{}{
	"Base.1.0.GeneralError":            {},
	"Base.1.0.ResourceNotFound":        {},
	"Base.1.0.MethodNotAllowed":        {},
	"Base.1.0.Unauthorized":            {},
	"Base.1.0.InternalError":           {},
	"Base.1.0.InsufficientPrivilege":   {},
	"Base.1.0.MalformedJSON":           {},
	"Base.1.0.PropertyMissing":         {},
	"Base.1.0.PropertyValueNotInList":  {},
	"Base.1.0.ResourceCannotBeCreated": {},
	"Base.1.0.NotImplemented":          {},
}

// Common error messages
const (
	errorUsernameAlreadyExists = "Username already exists"
)

// Handler implements the Redfish API endpoints
type Handler struct {
	db     *database.DB
	auth   *auth.Authenticator
	bmcSvc *bmc.Service
}

// New creates a new API handler
func New(db *database.DB) http.Handler {
	return NewRouter(db)
}

// handleRedfish routes Redfish API requests
func (h *Handler) handleRedfish(w http.ResponseWriter, r *http.Request) {
	// Correlation ID injection: accept inbound header or generate one.
	ctx, cid := ctxkeys.EnsureCorrelationID(r.Context())
	if hdrCID := r.Header.Get("X-Correlation-ID"); hdrCID != "" && cid == "" {
		ctx = ctxkeys.WithCorrelationID(ctx, hdrCID)
		cid = hdrCID
	}
	// Propagate into request context for downstream handlers
	r = r.WithContext(ctx)
	slog.Debug("Handling Redfish request", "method", r.Method, "path", strings.TrimPrefix(r.URL.Path, "/redfish"), "correlation_id", cid)
	path := strings.TrimPrefix(r.URL.Path, "/redfish")

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

	// Handle AccountService endpoints
	if strings.HasPrefix(path, "/v1/AccountService") {
		h.handleAccountService(w, r, path, user)
		return
	}

	// Handle EventService endpoints (stub)
	if strings.HasPrefix(path, "/v1/EventService") {
		h.handleEventService(w, r, path, user)
		return
	}

	// Handle TaskService endpoints (stub)
	if strings.HasPrefix(path, "/v1/TaskService") {
		h.handleTaskService(w, r, path, user)
		return
	}

	// Handle aggregator-specific endpoints
	h.handleAggregatorEndpoints(w, r, path, user)
}

// [rest of file unchanged]
