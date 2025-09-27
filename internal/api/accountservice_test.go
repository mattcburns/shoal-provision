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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
