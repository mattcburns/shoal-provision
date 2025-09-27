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
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"shoal/pkg/auth"
	"shoal/pkg/models"
)

func TestAccountService_RootAndRoles(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	// Login to get token
	loginBody, _ := json.Marshal(map[string]string{"UserName": "admin", "Password": "admin"})
	req := httptest.NewRequest(http.MethodPost, "/redfish/v1/SessionService/Sessions", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on login, got %d", rec.Code)
	}
	token := rec.Header().Get("X-Auth-Token")

	// AccountService root
	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/AccountService", nil)
	req.Header.Set("X-Auth-Token", token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for AccountService root, got %d", rec.Code)
	}

	// Roles collection
	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/AccountService/Roles", nil)
	req.Header.Set("X-Auth-Token", token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for Roles collection, got %d", rec.Code)
	}
}

func TestAccountService_AccountsCRUD_RBAC(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	// Admin login
	loginBody, _ := json.Marshal(map[string]string{"UserName": "admin", "Password": "admin"})
	req := httptest.NewRequest(http.MethodPost, "/redfish/v1/SessionService/Sessions", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 on admin login, got %d", rec.Code)
	}
	adminToken := rec.Header().Get("X-Auth-Token")

	// Create a viewer user via AccountService
	create := map[string]any{
		"UserName": "viewer1",
		"Password": "secret",
		"RoleId":   "ReadOnly",
		"Enabled":  true,
	}
	body, _ := json.Marshal(create)
	req = httptest.NewRequest(http.MethodPost, "/redfish/v1/AccountService/Accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 on create account, got %d: %s", rec.Code, rec.Body.String())
	}

	var acct map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &acct)
	acctID, _ := acct["Id"].(string)
	if acctID == "" {
		t.Fatalf("missing Id in created account")
	}

	// Get account as admin
	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/AccountService/Accounts/"+acctID, nil)
	req.Header.Set("X-Auth-Token", adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on get account, got %d", rec.Code)
	}

	// Patch account role to Operator
	patch := map[string]any{"RoleId": "Operator"}
	body, _ = json.Marshal(patch)
	req = httptest.NewRequest(http.MethodPatch, "/redfish/v1/AccountService/Accounts/"+acctID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on patch account, got %d: %s", rec.Code, rec.Body.String())
	}

	// Delete account
	req = httptest.NewRequest(http.MethodDelete, "/redfish/v1/AccountService/Accounts/"+acctID, nil)
	req.Header.Set("X-Auth-Token", adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on delete account, got %d", rec.Code)
	}
}

func TestAccountService_RBAC_Negatives(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	// Admin login
	loginBody, _ := json.Marshal(map[string]string{"UserName": "admin", "Password": "admin"})
	req := httptest.NewRequest(http.MethodPost, "/redfish/v1/SessionService/Sessions", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 on admin login, got %d", rec.Code)
	}
	adminToken := rec.Header().Get("X-Auth-Token")

	// Create a viewer user directly in DB
	ctx := context.Background()
	viewerPass := "viewerpass"
	hash, err := auth.HashPassword(viewerPass)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		t.Fatalf("failed to generate id: %v", err)
	}
	viewer := &models.User{
		ID:           hex.EncodeToString(idBytes),
		Username:     "viewerNeg",
		PasswordHash: hash,
		Role:         models.RoleViewer,
		Enabled:      true,
	}
	if err := db.CreateUser(ctx, viewer); err != nil {
		t.Fatalf("failed to create viewer: %v", err)
	}

	// Login as viewer
	loginBody, _ = json.Marshal(map[string]string{"UserName": viewer.Username, "Password": viewerPass})
	req = httptest.NewRequest(http.MethodPost, "/redfish/v1/SessionService/Sessions", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 on viewer login, got %d", rec.Code)
	}
	viewerToken := rec.Header().Get("X-Auth-Token")

	// 1) Viewer cannot list accounts
	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/AccountService/Accounts", nil)
	req.Header.Set("X-Auth-Token", viewerToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer listing accounts, got %d", rec.Code)
	}
	var errObj map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &errObj)
	if e, ok := errObj["error"].(map[string]any); ok {
		if e["code"] != "Base.1.0.InsufficientPrivilege" {
			t.Fatalf("expected InsufficientPrivilege code, got %v", e["code"])
		}
	}

	// 2) Viewer cannot create accounts
	create := map[string]any{"UserName": "noop", "Password": "x", "RoleId": "ReadOnly"}
	body, _ := json.Marshal(create)
	req = httptest.NewRequest(http.MethodPost, "/redfish/v1/AccountService/Accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", viewerToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer creating account, got %d", rec.Code)
	}

	// 3) Viewer cannot fetch an individual account (even own)
	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/AccountService/Accounts/"+viewer.ID, nil)
	req.Header.Set("X-Auth-Token", viewerToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer reading account, got %d", rec.Code)
	}

	// 4) Unauthenticated access gets 401
	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/AccountService/Accounts", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated, got %d", rec.Code)
	}

	// 5) Admin cannot delete admin user
	adminUser, err := db.GetUserByUsername(ctx, "admin")
	if err != nil || adminUser == nil {
		t.Fatalf("failed to get admin user: %v", err)
	}
	req = httptest.NewRequest(http.MethodDelete, "/redfish/v1/AccountService/Accounts/"+adminUser.ID, nil)
	req.Header.Set("X-Auth-Token", adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when deleting admin, got %d", rec.Code)
	}

	// 6) Admin cannot disable admin user
	patch := map[string]any{"Enabled": false}
	body, _ = json.Marshal(patch)
	req = httptest.NewRequest(http.MethodPatch, "/redfish/v1/AccountService/Accounts/"+adminUser.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when disabling admin, got %d", rec.Code)
	}

	// 7) Duplicate username on create returns 409
	dup := map[string]any{"UserName": viewer.Username, "Password": "secret2", "RoleId": "ReadOnly"}
	body, _ = json.Marshal(dup)
	req = httptest.NewRequest(http.MethodPost, "/redfish/v1/AccountService/Accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate username, got %d", rec.Code)
	}

	// 8) Invalid role on create returns 400
	bad := map[string]any{"UserName": "badrole", "Password": "p", "RoleId": "NotARole"}
	body, _ = json.Marshal(bad)
	req = httptest.NewRequest(http.MethodPost, "/redfish/v1/AccountService/Accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid role, got %d", rec.Code)
	}

}
