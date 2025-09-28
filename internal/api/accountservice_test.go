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

func loginAndGetToken(t *testing.T, handler http.Handler, username, password string) string {
	t.Helper()

	body, err := json.Marshal(map[string]string{
		"UserName": username,
		"Password": password,
	})
	if err != nil {
		t.Fatalf("failed to marshal login body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/redfish/v1/SessionService/Sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 from login, got %d", rec.Code)
	}
	token := rec.Header().Get("X-Auth-Token")
	if token == "" {
		t.Fatalf("login response missing X-Auth-Token header")
	}
	return token
}

func createUser(t *testing.T, db testDatabase, username, password, role string) *models.User {
	t.Helper()

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		t.Fatalf("failed to generate user id: %v", err)
	}
	user := &models.User{
		ID:           hex.EncodeToString(idBytes),
		Username:     username,
		PasswordHash: hash,
		Role:         role,
		Enabled:      true,
	}
	if err := db.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("failed to create user %s: %v", username, err)
	}
	return user
}

type testDatabase interface {
	CreateUser(ctx context.Context, user *models.User) error
	GetUserByUsername(ctx context.Context, username string) (*models.User, error)
	GetUser(ctx context.Context, id string) (*models.User, error)
	DeleteUser(ctx context.Context, id string) error
}

func TestAccountServiceRootAndCRUD(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	// unauthenticated access should be rejected
	req := httptest.NewRequest(http.MethodGet, "/redfish/v1/AccountService", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated AccountService root, got %d", rec.Code)
	}

	adminToken := loginAndGetToken(t, handler, "admin", "admin")

	// root fetch succeeds with token
	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/AccountService", nil)
	req.Header.Set("X-Auth-Token", adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for AccountService root, got %d", rec.Code)
	}
	var svc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &svc); err != nil {
		t.Fatalf("failed to parse AccountService root: %v", err)
	}
	if svc["Accounts"].(map[string]any)["@odata.id"] != "/redfish/v1/AccountService/Accounts" {
		t.Fatalf("expected Accounts link in AccountService root")
	}

	// GET accounts collection (should contain admin)
	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/AccountService/Accounts", nil)
	req.Header.Set("X-Auth-Token", adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from accounts collection, got %d", rec.Code)
	}
	var coll map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &coll); err != nil {
		t.Fatalf("failed to parse accounts collection: %v", err)
	}
	members, ok := coll["Members"].([]any)
	if !ok || len(members) != 1 {
		t.Fatalf("expected admin account present, got %v", coll["Members"])
	}

	// create a new account
	createBody, _ := json.Marshal(map[string]any{
		"UserName": "operator1",
		"Password": "op-pass",
		"RoleId":   "Operator",
	})
	req = httptest.NewRequest(http.MethodPost, "/redfish/v1/AccountService/Accounts", bytes.NewReader(createBody))
	req.Header.Set("X-Auth-Token", adminToken)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 on create account, got %d", rec.Code)
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to parse created account: %v", err)
	}
	accountID, _ := created["Id"].(string)
	if accountID == "" {
		t.Fatalf("missing account Id in create response")
	}

	// fetch account
	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/AccountService/Accounts/"+accountID, nil)
	req.Header.Set("X-Auth-Token", adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 retrieving account, got %d", rec.Code)
	}

	// patch account: disable and change role + password
	patchBody, _ := json.Marshal(map[string]any{
		"Enabled":  true,
		"RoleId":   "ReadOnly",
		"Password": "new-pass",
	})
	req = httptest.NewRequest(http.MethodPatch, "/redfish/v1/AccountService/Accounts/"+accountID, bytes.NewReader(patchBody))
	req.Header.Set("X-Auth-Token", adminToken)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from patch, got %d", rec.Code)
	}
	var patched map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &patched); err != nil {
		t.Fatalf("failed to parse patched account: %v", err)
	}
	if patched["RoleId"] != "ReadOnly" || patched["Enabled"].(bool) != true {
		t.Fatalf("patch did not apply expected changes: %v", patched)
	}

	// new password should work
	loginAndGetToken(t, handler, "operator1", "new-pass")

	// delete account
	req = httptest.NewRequest(http.MethodDelete, "/redfish/v1/AccountService/Accounts/"+accountID, nil)
	req.Header.Set("X-Auth-Token", adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on delete account, got %d", rec.Code)
	}

	// subsequent fetch should 404
	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/AccountService/Accounts/"+accountID, nil)
	req.Header.Set("X-Auth-Token", adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rec.Code)
	}
}

func TestAccountServiceRequiresAdminForMutations(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	createUser(t, db, "viewer", "viewer-pass", models.RoleViewer)
	viewerToken := loginAndGetToken(t, handler, "viewer", "viewer-pass")

	// GET collection as viewer should be forbidden
	req := httptest.NewRequest(http.MethodGet, "/redfish/v1/AccountService/Accounts", nil)
	req.Header.Set("X-Auth-Token", viewerToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin listing accounts, got %d", rec.Code)
	}

	// POST should also be forbidden
	body, _ := json.Marshal(map[string]any{"UserName": "user2", "Password": "pw", "RoleId": "Operator"})
	req = httptest.NewRequest(http.MethodPost, "/redfish/v1/AccountService/Accounts", bytes.NewReader(body))
	req.Header.Set("X-Auth-Token", viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin create, got %d", rec.Code)
	}
}

func TestAccountServiceRolesEndpoints(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	token := loginAndGetToken(t, handler, "admin", "admin")

	req := httptest.NewRequest(http.MethodGet, "/redfish/v1/AccountService/Roles", nil)
	req.Header.Set("X-Auth-Token", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for roles collection, got %d", rec.Code)
	}
	var coll map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &coll); err != nil {
		t.Fatalf("failed to parse roles collection: %v", err)
	}
	if coll["Members@odata.count"] != float64(3) {
		t.Fatalf("expected 3 roles, got %v", coll["Members@odata.count"])
	}

	tests := map[string][]string{
		"Administrator": {"Login", "ConfigureManager", "ConfigureUsers", "ConfigureComponents", "ConfigureSelf"},
		"Operator":      {"Login", "ConfigureComponents", "ConfigureSelf"},
		"ReadOnly":      {"Login", "ConfigureSelf"},
	}

	for roleID, expected := range tests {
		req = httptest.NewRequest(http.MethodGet, "/redfish/v1/AccountService/Roles/"+roleID, nil)
		req.Header.Set("X-Auth-Token", token)
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for role %s, got %d", roleID, rec.Code)
		}
		var role map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &role); err != nil {
			t.Fatalf("failed to parse role %s: %v", roleID, err)
		}
		privAny, ok := role["AssignedPrivileges"].([]any)
		if !ok {
			t.Fatalf("role %s missing AssignedPrivileges", roleID)
		}
		if len(privAny) != len(expected) {
			t.Fatalf("role %s expected %d privileges, got %d", roleID, len(expected), len(privAny))
		}
		for i, v := range expected {
			if privAny[i] != v {
				t.Fatalf("role %s privilege mismatch at %d: want %s got %v", roleID, i, v, privAny[i])
			}
		}
	}
}

func TestAccountServiceValidationErrors(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	token := loginAndGetToken(t, handler, "admin", "admin")

	// Missing required fields -> PropertyMissing
	req := httptest.NewRequest(http.MethodPost, "/redfish/v1/AccountService/Accounts", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Auth-Token", token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing properties, got %d", rec.Code)
	}
	var errResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	ext := extractFirstExtendedInfo(t, errResp)
	if ext["MessageId"] != "Base.1.0.PropertyMissing" {
		t.Fatalf("expected PropertyMissing MessageId, got %v", ext["MessageId"])
	}

	// Unsupported role -> PropertyValueNotInList
	body, _ := json.Marshal(map[string]any{"UserName": "badrole", "Password": "pw", "RoleId": "Unknown"})
	req = httptest.NewRequest(http.MethodPost, "/redfish/v1/AccountService/Accounts", bytes.NewReader(body))
	req.Header.Set("X-Auth-Token", token)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsupported role, got %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	ext = extractFirstExtendedInfo(t, errResp)
	if ext["MessageId"] != "Base.1.0.PropertyValueNotInList" {
		t.Fatalf("expected PropertyValueNotInList MessageId, got %v", ext["MessageId"])
	}
}

func extractFirstExtendedInfo(t *testing.T, errResp map[string]any) map[string]any {
	t.Helper()
	errObj, ok := errResp["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error object: %v", errResp)
	}
	extSlice, ok := errObj["@Message.ExtendedInfo"].([]any)
	if !ok || len(extSlice) == 0 {
		t.Fatalf("missing ExtendedInfo: %v", errObj)
	}
	first, ok := extSlice[0].(map[string]any)
	if !ok {
		t.Fatalf("invalid ExtendedInfo entry: %v", extSlice[0])
	}
	return first
}
