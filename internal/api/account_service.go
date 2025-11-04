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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	authpkg "shoal/pkg/auth"
	"shoal/pkg/models"
	"shoal/pkg/redfish"
)

// handleAccountService routes and implements AccountService endpoints.
// Extracted from api.go per design 019. Behavior is unchanged.
func (h *Handler) handleAccountService(w http.ResponseWriter, r *http.Request, path string, user *models.User) {
	// Remove /v1/AccountService prefix
	subPath := strings.TrimPrefix(path, "/v1/AccountService")

	// Root resource
	if subPath == "" || subPath == "/" {
		if r.Method == http.MethodOptions {
			h.writeAllow(w, http.MethodGet)
			return
		}
		if r.Method != http.MethodGet {
			h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
			return
		}
		svc := redfish.AccountService{
			ODataContext:   "/redfish/v1/$metadata#AccountService.AccountService",
			ODataID:        "/redfish/v1/AccountService",
			ODataType:      "#AccountService.v1_0_0.AccountService",
			ID:             "AccountService",
			Name:           "Account Service",
			ServiceEnabled: true,
			Accounts:       redfish.ODataIDRef{ODataID: "/redfish/v1/AccountService/Accounts"},
			Roles:          redfish.ODataIDRef{ODataID: "/redfish/v1/AccountService/Roles"},
		}
		h.writeJSONResponse(w, http.StatusOK, svc)
		return
	}

	// Accounts collection
	if subPath == "/Accounts" || subPath == "/Accounts/" {
		if r.Method == http.MethodOptions {
			h.writeAllow(w, http.MethodGet, http.MethodPost)
			return
		}
		switch r.Method {
		case http.MethodGet:
			// Admin only
			if !authpkg.IsAdmin(user) {
				h.writeErrorResponse(w, http.StatusForbidden, "Base.1.0.InsufficientPrivilege", "Administrator privilege required")
				return
			}
			h.handleGetAccountsCollection(w, r)
			return
		case http.MethodPost:
			if !authpkg.IsAdmin(user) {
				h.writeErrorResponse(w, http.StatusForbidden, "Base.1.0.InsufficientPrivilege", "Administrator privilege required")
				return
			}
			h.handleCreateAccount(w, r)
			return
		default:
			h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
			return
		}
	}

	// Individual account
	if strings.HasPrefix(subPath, "/Accounts/") {
		if r.Method == http.MethodOptions {
			h.writeAllow(w, http.MethodGet, http.MethodPatch, http.MethodDelete)
			return
		}
		if !authpkg.IsAdmin(user) {
			h.writeErrorResponse(w, http.StatusForbidden, "Base.1.0.InsufficientPrivilege", "Administrator privilege required")
			return
		}
		id := strings.Trim(strings.TrimPrefix(subPath, "/Accounts/"), "/")
		if id == "" {
			h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Account not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			h.handleGetAccount(w, r, id)
			return
		case http.MethodPatch:
			h.handlePatchAccount(w, r, id)
			return
		case http.MethodDelete:
			h.handleDeleteAccount(w, r, id)
			return
		default:
			h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
			return
		}
	}

	// Roles endpoints
	if subPath == "/Roles" || subPath == "/Roles/" {
		if r.Method == http.MethodOptions {
			h.writeAllow(w, http.MethodGet)
			return
		}
		if r.Method != http.MethodGet {
			h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
			return
		}
		h.handleGetRolesCollection(w, r)
		return
	}
	if strings.HasPrefix(subPath, "/Roles/") {
		if r.Method == http.MethodOptions {
			h.writeAllow(w, http.MethodGet)
			return
		}
		if r.Method != http.MethodGet {
			h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
			return
		}
		roleID := strings.Trim(strings.TrimPrefix(subPath, "/Roles/"), "/")
		h.handleGetRole(w, r, roleID)
		return
	}

	h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Resource not found")
}

// handleGetAccountsCollection returns the Accounts collection.
// Extracted from api.go per design 019. Behavior is unchanged.
func (h *Handler) handleGetAccountsCollection(w http.ResponseWriter, r *http.Request) {
	users, err := h.db.GetUsers(r.Context())
	if err != nil {
		slog.Error("Failed to get users", "error", err)
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to retrieve users")
		return
	}
	var members []redfish.ODataIDRef
	for _, u := range users {
		members = append(members, redfish.ODataIDRef{ODataID: fmt.Sprintf("/redfish/v1/AccountService/Accounts/%s", u.ID)})
	}
	coll := redfish.Collection{
		ODataContext: "/redfish/v1/$metadata#ManagerAccountCollection.ManagerAccountCollection",
		ODataID:      "/redfish/v1/AccountService/Accounts",
		ODataType:    "#ManagerAccountCollection.ManagerAccountCollection",
		Name:         "Manager Account Collection",
		Members:      members,
		MembersCount: len(members),
	}
	etag := accountsCollectionETag(users)
	if match := r.Header.Get("If-None-Match"); match != "" && ifNoneMatchMatches(match, etag) {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.writeJSONResponseWithETag(w, http.StatusOK, coll, etag)
}

// handleCreateAccount creates a new user account.
// Extracted from api.go per design 019. Behavior is unchanged.
func (h *Handler) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserName string `json:"UserName"`
		RoleID   string `json:"RoleId"`
		Password string `json:"Password"`
		Enabled  *bool  `json:"Enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorResponse(w, http.StatusBadRequest, "Base.1.0.MalformedJSON", "Invalid JSON in request body")
		return
	}
	if strings.TrimSpace(req.UserName) == "" || req.Password == "" || strings.TrimSpace(req.RoleID) == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Base.1.0.PropertyMissing", "UserName, Password and RoleId are required")
		return
	}
	// Check existing username
	if existing, _ := h.db.GetUserByUsername(r.Context(), req.UserName); existing != nil {
		h.writeErrorResponse(w, http.StatusConflict, "Base.1.0.ResourceCannotBeCreated", errorUsernameAlreadyExists)
		return
	}
	// Map role
	role, ok := modelsRoleFromRedfish(req.RoleID)
	if !ok {
		h.writeErrorResponse(w, http.StatusBadRequest, "Base.1.0.PropertyValueNotInList", "Unsupported RoleId")
		return
	}
	// Hash password
	pwHash, err := authpkg.HashPassword(req.Password)
	if err != nil {
		slog.Error("Failed to hash password", "error", err)
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to create account")
		return
	}
	// Generate user ID
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		slog.Error("Failed to generate user ID", "error", err)
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to create account")
		return
	}
	user := &models.User{
		ID:           hex.EncodeToString(idBytes),
		Username:     req.UserName,
		PasswordHash: pwHash,
		Role:         role,
		Enabled:      true,
	}
	if req.Enabled != nil {
		user.Enabled = *req.Enabled
	}
	if err := h.db.CreateUser(r.Context(), user); err != nil {
		slog.Error("Failed to create user", "error", err)
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to create account")
		return
	}
	resp := toRedfishAccount(user)
	etag := accountETag(user)
	w.Header().Set("Location", resp.ODataID)
	h.writeJSONResponseWithETag(w, http.StatusCreated, resp, etag)
}

// handleGetAccount returns an individual account.
// Extracted from api.go per design 019. Behavior is unchanged.
func (h *Handler) handleGetAccount(w http.ResponseWriter, r *http.Request, id string) {
	u, err := h.db.GetUser(r.Context(), id)
	if err != nil {
		slog.Error("Failed to get user", "id", id, "error", err)
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to retrieve account")
		return
	}
	if u == nil {
		h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Account not found")
		return
	}
	resp := toRedfishAccount(u)
	etag := accountETag(u)
	if match := r.Header.Get("If-None-Match"); match != "" && ifNoneMatchMatches(match, etag) {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.writeJSONResponseWithETag(w, http.StatusOK, resp, etag)
}

// handlePatchAccount updates fields on an account.
// Extracted from api.go per design 019. Behavior is unchanged.
func (h *Handler) handlePatchAccount(w http.ResponseWriter, r *http.Request, id string) {
	u, err := h.db.GetUser(r.Context(), id)
	if err != nil || u == nil {
		h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Account not found")
		return
	}
	var patch map[string]any
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		h.writeErrorResponse(w, http.StatusBadRequest, "Base.1.0.MalformedJSON", "Invalid JSON in request body")
		return
	}
	if v, ok := patch["UserName"].(string); ok && strings.TrimSpace(v) != "" {
		// Validate uniqueness
		if existing, _ := h.db.GetUserByUsername(r.Context(), v); existing != nil && existing.ID != u.ID {
			h.writeErrorResponse(w, http.StatusConflict, "Base.1.0.ResourceCannotBeCreated", errorUsernameAlreadyExists)
			return
		}
		u.Username = v
	}
	if v, ok := patch["Enabled"].(bool); ok {
		u.Enabled = v
	}
	if v, ok := patch["RoleId"].(string); ok {
		role, ok2 := modelsRoleFromRedfish(v)
		if !ok2 {
			h.writeErrorResponse(w, http.StatusBadRequest, "Base.1.0.PropertyValueNotInList", "Unsupported RoleId")
			return
		}
		u.Role = role
	}
	if v, ok := patch["Password"].(string); ok && v != "" {
		pwHash, err := authpkg.HashPassword(v)
		if err != nil {
			h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to update account")
			return
		}
		u.PasswordHash = pwHash
	}
	if u.Username == "admin" && !u.Enabled {
		h.writeErrorResponse(w, http.StatusBadRequest, "Base.1.0.GeneralError", "Cannot disable admin user")
		return
	}
	if err := h.db.UpdateUser(r.Context(), u); err != nil {
		slog.Error("Failed to update user", "id", id, "error", err)
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to update account")
		return
	}
	updated, err := h.db.GetUser(r.Context(), id)
	if err != nil || updated == nil {
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to reload account")
		return
	}
	resp := toRedfishAccount(updated)
	etag := accountETag(updated)
	h.writeJSONResponseWithETag(w, http.StatusOK, resp, etag)
}

// handleDeleteAccount deletes a user account.
// Extracted from api.go per design 019. Behavior is unchanged.
func (h *Handler) handleDeleteAccount(w http.ResponseWriter, r *http.Request, id string) {
	u, err := h.db.GetUser(r.Context(), id)
	if err != nil || u == nil {
		h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Account not found")
		return
	}
	if u.Username == "admin" {
		h.writeErrorResponse(w, http.StatusBadRequest, "Base.1.0.GeneralError", "Cannot delete admin user")
		return
	}
	if err := h.db.DeleteUser(r.Context(), id); err != nil {
		slog.Error("Failed to delete user", "id", id, "error", err)
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to delete account")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGetRolesCollection returns the Roles collection.
// Extracted from api.go per design 019. Behavior is unchanged.
func (h *Handler) handleGetRolesCollection(w http.ResponseWriter, r *http.Request) {
	members := []redfish.ODataIDRef{
		{ODataID: "/redfish/v1/AccountService/Roles/Administrator"},
		{ODataID: "/redfish/v1/AccountService/Roles/Operator"},
		{ODataID: "/redfish/v1/AccountService/Roles/ReadOnly"},
	}
	coll := redfish.Collection{
		ODataContext: "/redfish/v1/$metadata#RoleCollection.RoleCollection",
		ODataID:      "/redfish/v1/AccountService/Roles",
		ODataType:    "#RoleCollection.RoleCollection",
		Name:         "Role Collection",
		Members:      members,
		MembersCount: len(members),
	}
	h.writeJSONResponse(w, http.StatusOK, coll)
}

// handleGetRole returns a specific role.
// Extracted from api.go per design 019. Behavior is unchanged.
func (h *Handler) handleGetRole(w http.ResponseWriter, r *http.Request, roleID string) {
	norm := strings.ToLower(roleID)
	switch norm {
	case "administrator":
		h.writeJSONResponse(w, http.StatusOK, redfish.Role{
			ODataContext:       "/redfish/v1/$metadata#Role.Role",
			ODataID:            "/redfish/v1/AccountService/Roles/Administrator",
			ODataType:          "#Role.v1_0_0.Role",
			ID:                 "Administrator",
			Name:               "Administrator",
			IsPredefined:       true,
			AssignedPrivileges: []string{"Login", "ConfigureManager", "ConfigureUsers", "ConfigureComponents", "ConfigureSelf"},
		})
	case "operator":
		h.writeJSONResponse(w, http.StatusOK, redfish.Role{
			ODataContext:       "/redfish/v1/$metadata#Role.Role",
			ODataID:            "/redfish/v1/AccountService/Roles/Operator",
			ODataType:          "#Role.v1_0_0.Role",
			ID:                 "Operator",
			Name:               "Operator",
			IsPredefined:       true,
			AssignedPrivileges: []string{"Login", "ConfigureComponents", "ConfigureSelf"},
		})
	case "readonly", "read-only", "read_only", "viewer":
		h.writeJSONResponse(w, http.StatusOK, redfish.Role{
			ODataContext:       "/redfish/v1/$metadata#Role.Role",
			ODataID:            "/redfish/v1/AccountService/Roles/ReadOnly",
			ODataType:          "#Role.v1_0_0.Role",
			ID:                 "ReadOnly",
			Name:               "ReadOnly",
			IsPredefined:       true,
			AssignedPrivileges: []string{"Login", "ConfigureSelf"},
		})
	default:
		h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Role not found")
	}
}
