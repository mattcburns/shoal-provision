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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"shoal/pkg/models"
)

func TestOptionsServiceRoot(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	req := httptest.NewRequest(http.MethodOptions, "/redfish/v1/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content, got %d", rec.Code)
	}
	allow := rec.Header().Get("Allow")
	if allow == "" || allow != http.MethodGet {
		t.Fatalf("expected Allow: GET, got %q", allow)
	}
	if rec.Header().Get("OData-Version") != "4.0" {
		t.Fatalf("expected OData-Version header on OPTIONS response")
	}
}

func TestOptionsAccountServiceEndpoints(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	// Login to get token
	body, _ := json.Marshal(map[string]string{"UserName": "admin", "Password": "admin"})
	req := httptest.NewRequest(http.MethodPost, "/redfish/v1/SessionService/Sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("login failed: %d", rec.Code)
	}
	token := rec.Header().Get("X-Auth-Token")

	// Accounts collection
	req = httptest.NewRequest(http.MethodOptions, "/redfish/v1/AccountService/Accounts", nil)
	req.Header.Set("X-Auth-Token", token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 OPTIONS, got %d", rec.Code)
	}
	allow := rec.Header().Get("Allow")
	expected := []string{http.MethodGet, http.MethodPost}
	for _, m := range expected {
		if !strings.Contains(allow, m) {
			t.Fatalf("expected Allow to contain %s; got %q", m, allow)
		}
	}

	// Create an account to exercise individual resource OPTIONS
	accBody, _ := json.Marshal(map[string]any{"UserName": "user1", "Password": "pw", "RoleId": "ReadOnly"})
	req = httptest.NewRequest(http.MethodPost, "/redfish/v1/AccountService/Accounts", bytes.NewReader(accBody))
	req.Header.Set("X-Auth-Token", token)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create account failed: %d", rec.Code)
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id, _ := created["Id"].(string)
	if id == "" {
		t.Fatalf("missing Id for created account")
	}

	req = httptest.NewRequest(http.MethodOptions, "/redfish/v1/AccountService/Accounts/"+id, nil)
	req.Header.Set("X-Auth-Token", token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 OPTIONS on account resource, got %d", rec.Code)
	}
	allow = rec.Header().Get("Allow")
	for _, m := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		if !strings.Contains(allow, m) {
			t.Fatalf("expected Allow to contain %s; got %q", m, allow)
		}
	}
}

func TestOptionsSessionService(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	// OPTIONS should be allowed without auth for discovery
	req := httptest.NewRequest(http.MethodOptions, "/redfish/v1/SessionService/Sessions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 OPTIONS, got %d", rec.Code)
	}
	allow := rec.Header().Get("Allow")
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		if !strings.Contains(allow, m) {
			t.Fatalf("expected Allow to contain %s; got %q", m, allow)
		}
	}
}

func TestOptionsAggregationServiceConnectionMethods(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	body, _ := json.Marshal(map[string]string{"UserName": "admin", "Password": "admin"})
	req := httptest.NewRequest(http.MethodPost, "/redfish/v1/SessionService/Sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("login failed: %d", rec.Code)
	}
	token := rec.Header().Get("X-Auth-Token")

	method := &models.ConnectionMethod{
		ID:                   "cm-options",
		Name:                 "Options Method",
		ConnectionMethodType: "Redfish",
		Address:              "https://bmc-options.example.com",
		Username:             "admin",
		Password:             "secret",
		Enabled:              true,
	}
	if err := db.CreateConnectionMethod(context.Background(), method); err != nil {
		t.Fatalf("failed to seed connection method: %v", err)
	}

	req = httptest.NewRequest(http.MethodOptions, "/redfish/v1/AggregationService/ConnectionMethods", nil)
	req.Header.Set("X-Auth-Token", token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 OPTIONS on collection, got %d", rec.Code)
	}
	allow := rec.Header().Get("Allow")
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		if !strings.Contains(allow, m) {
			t.Fatalf("expected Allow header to include %s; got %q", m, allow)
		}
	}

	req = httptest.NewRequest(http.MethodOptions, "/redfish/v1/AggregationService/ConnectionMethods/"+method.ID, nil)
	req.Header.Set("X-Auth-Token", token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 OPTIONS on resource, got %d", rec.Code)
	}
	allow = rec.Header().Get("Allow")
	for _, m := range []string{http.MethodGet, http.MethodDelete} {
		if !strings.Contains(allow, m) {
			t.Fatalf("expected Allow header to include %s; got %q", m, allow)
		}
	}
}
