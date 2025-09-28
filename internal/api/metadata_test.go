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

func TestMetadataEndpoint(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	// $metadata should be accessible without auth and return XML + OData header
	req := httptest.NewRequest(http.MethodGet, "/redfish/v1/$metadata", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from $metadata, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/xml" {
		t.Fatalf("expected Content-Type application/xml, got %q", ct)
	}
	if od := rec.Header().Get("OData-Version"); od != "4.0" {
		t.Fatalf("expected OData-Version 4.0, got %q", od)
	}
	if body := rec.Body.String(); body == "" {
		t.Fatalf("expected non-empty metadata body")
	}
}

func TestMetadataETagConditionalGet(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	// First request to get ETag
	req1 := httptest.NewRequest(http.MethodGet, "/redfish/v1/$metadata", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200 from $metadata, got %d", rec1.Code)
	}
	etag := rec1.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("expected ETag header on first response")
	}

	// Second request with If-None-Match should yield 304
	req2 := httptest.NewRequest(http.MethodGet, "/redfish/v1/$metadata", nil)
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusNotModified {
		t.Fatalf("expected 304 Not Modified when ETag matches, got %d", rec2.Code)
	}
}

func TestRegistriesETagConditionalGet(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	// Login to access /Registries endpoints
	loginBody, _ := json.Marshal(map[string]string{"UserName": "admin", "Password": "admin"})
	req := httptest.NewRequest(http.MethodPost, "/redfish/v1/SessionService/Sessions", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on login, got %d", rec.Code)
	}
	token := rec.Header().Get("X-Auth-Token")

	// First request: fetch Base registry (we embed a minimal Base.json)
	req1 := httptest.NewRequest(http.MethodGet, "/redfish/v1/Registries/Base", nil)
	req1.Header.Set("X-Auth-Token", token)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200 for Base registry, got %d", rec1.Code)
	}
	etag := rec1.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("expected ETag header for Base registry")
	}
	if ct := rec1.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %q", ct)
	}
	if od := rec1.Header().Get("OData-Version"); od != "4.0" {
		t.Fatalf("expected OData-Version 4.0, got %q", od)
	}

	// Second request with If-None-Match should return 304
	req2 := httptest.NewRequest(http.MethodGet, "/redfish/v1/Registries/Base", nil)
	req2.Header.Set("X-Auth-Token", token)
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotModified {
		t.Fatalf("expected 304 Not Modified for Base registry, got %d", rec2.Code)
	}
}
