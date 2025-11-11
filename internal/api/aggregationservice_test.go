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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"shoal/pkg/models"
)

func TestConnectionMethodsETags(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	adminToken := loginAndGetToken(t, handler, "admin", "admin")

	method := &models.ConnectionMethod{
		ID:                   "cm-test",
		Name:                 "Test Connection Method",
		ConnectionMethodType: "Redfish",
		Address:              "https://example.com",
		Username:             "admin",
		Password:             "secret",
		Enabled:              true,
	}
	if err := db.CreateConnectionMethod(context.Background(), method); err != nil {
		t.Fatalf("failed to seed connection method: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/redfish/v1/AggregationService/ConnectionMethods", nil)
	req.Header.Set("X-Auth-Token", adminToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from connection methods collection, got %d", rec.Code)
	}
	collectionETag := rec.Header().Get("ETag")
	if collectionETag == "" {
		t.Fatalf("expected ETag header on connection methods collection")
	}

	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/AggregationService/ConnectionMethods", nil)
	req.Header.Set("X-Auth-Token", adminToken)
	req.Header.Set("If-None-Match", collectionETag)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("expected 304 when collection ETag matches, got %d", rec.Code)
	}

	resourceURL := "/redfish/v1/AggregationService/ConnectionMethods/" + method.ID
	req = httptest.NewRequest(http.MethodGet, resourceURL, nil)
	req.Header.Set("X-Auth-Token", adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when fetching connection method, got %d", rec.Code)
	}
	resourceETag := rec.Header().Get("ETag")
	if resourceETag == "" {
		t.Fatalf("expected ETag header on connection method resource")
	}

	req = httptest.NewRequest(http.MethodGet, resourceURL, nil)
	req.Header.Set("X-Auth-Token", adminToken)
	req.Header.Set("If-None-Match", resourceETag)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("expected 304 when resource ETag matches, got %d", rec.Code)
	}

	time.Sleep(time.Second)

	managers := `[{"@odata.id": "/redfish/v1/Managers/M1"}]`
	systems := `[{"@odata.id": "/redfish/v1/Systems/S1"}]`
	if err := db.UpdateConnectionMethodAggregatedData(context.Background(), method.ID, managers, systems); err != nil {
		t.Fatalf("failed to update aggregated data: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, resourceURL, nil)
	req.Header.Set("X-Auth-Token", adminToken)
	req.Header.Set("If-None-Match", resourceETag)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when using stale ETag after update, got %d", rec.Code)
	}
	updatedETag := rec.Header().Get("ETag")
	if updatedETag == resourceETag {
		t.Fatalf("expected connection method ETag to change after update")
	}
}

func TestHandleAggregationService(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	adminToken := loginAndGetToken(t, handler, "admin", "admin")

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:           "GET aggregation service root",
			method:         http.MethodGet,
			path:           "/redfish/v1/AggregationService",
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var aggSvc map[string]interface{}
				if err := json.Unmarshal(rec.Body.Bytes(), &aggSvc); err != nil {
					t.Fatalf("failed to parse response: %v", err)
				}
				if aggSvc["@odata.type"] != "#AggregationService.v1_0_0.AggregationService" {
					t.Errorf("unexpected @odata.type: %v", aggSvc["@odata.type"])
				}
				if aggSvc["Id"] != "AggregationService" {
					t.Errorf("unexpected Id: %v", aggSvc["Id"])
				}
				connMethods, ok := aggSvc["ConnectionMethods"].(map[string]interface{})
				if !ok {
					t.Fatalf("expected ConnectionMethods object")
				}
				if connMethods["@odata.id"] != "/redfish/v1/AggregationService/ConnectionMethods" {
					t.Errorf("unexpected ConnectionMethods @odata.id: %v", connMethods["@odata.id"])
				}
			},
		},
		{
			name:           "OPTIONS aggregation service root",
			method:         http.MethodOptions,
			path:           "/redfish/v1/AggregationService",
			expectedStatus: http.StatusNoContent,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
				if allow := rec.Header().Get("Allow"); allow != "GET" {
					t.Errorf("expected Allow header 'GET', got %q", allow)
				}
			},
		},
		{
			name:           "POST aggregation service root (not allowed)",
			method:         http.MethodPost,
			path:           "/redfish/v1/AggregationService",
			expectedStatus: http.StatusMethodNotAllowed,
			checkResponse:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("X-Auth-Token", adminToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			if tt.checkResponse != nil {
				tt.checkResponse(t, rec)
			}
		})
	}
}

func TestHandleConnectionMethodsCollection(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	adminToken := loginAndGetToken(t, handler, "admin", "admin")

	tests := []struct {
		name           string
		method         string
		body           string
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:           "GET empty collection",
			method:         http.MethodGet,
			body:           "",
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var coll map[string]interface{}
				if err := json.Unmarshal(rec.Body.Bytes(), &coll); err != nil {
					t.Fatalf("failed to parse response: %v", err)
				}
				if count, _ := coll["Members@odata.count"].(float64); count != 0 {
					t.Errorf("expected 0 members, got %v", count)
				}
			},
		},
		{
			name:           "OPTIONS collection",
			method:         http.MethodOptions,
			body:           "",
			expectedStatus: http.StatusNoContent,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
				allow := rec.Header().Get("Allow")
				if allow != "GET, POST" {
					t.Errorf("expected Allow header 'GET, POST', got %q", allow)
				}
			},
		},
		{
			name:           "DELETE collection (not allowed)",
			method:         http.MethodDelete,
			body:           "",
			expectedStatus: http.StatusMethodNotAllowed,
			checkResponse:  nil,
		},
		{
			name:           "POST with malformed JSON",
			method:         http.MethodPost,
			body:           `{invalid json}`,
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var errResp map[string]interface{}
				if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
					t.Fatalf("failed to parse error response: %v", err)
				}
				errObj, ok := errResp["error"].(map[string]interface{})
				if !ok {
					t.Error("expected error object in response")
				} else if code, _ := errObj["code"].(string); code != "Base.1.0.MalformedJSON" {
					t.Errorf("expected error code Base.1.0.MalformedJSON, got %q", code)
				}
			},
		},
		{
			name:           "POST with missing required fields",
			method:         http.MethodPost,
			body:           `{"Name":"test"}`,
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var errResp map[string]interface{}
				if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
					t.Fatalf("failed to parse error response: %v", err)
				}
				errObj, ok := errResp["error"].(map[string]interface{})
				if !ok {
					t.Error("expected error object in response")
				} else if code, _ := errObj["code"].(string); code != "Base.1.0.PropertyMissing" {
					t.Errorf("expected error code Base.1.0.PropertyMissing, got %q", code)
				}
			},
		},
		{
			name:   "POST with invalid BMC address",
			method: http.MethodPost,
			body: `{
				"Name": "test-bmc",
				"ConnectionMethodVariant.Address": "http://invalid-bmc-address:8080",
				"ConnectionMethodVariant.Authentication": {
					"Username": "admin",
					"Password": "password"
				}
			}`,
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var errResp map[string]interface{}
				if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
					t.Fatalf("failed to parse error response: %v", err)
				}
				errObj, ok := errResp["error"].(map[string]interface{})
				if !ok {
					t.Error("expected error object in response")
				}
				// Should get ResourceCannotBeCreated or InternalError
				code, _ := errObj["code"].(string)
				if code != "Base.1.0.ResourceCannotBeCreated" && code != "Base.1.0.InternalError" {
					t.Errorf("expected error code Base.1.0.ResourceCannotBeCreated or InternalError, got %q", code)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req *http.Request
			if tt.body != "" {
				req = httptest.NewRequest(tt.method, "/redfish/v1/AggregationService/ConnectionMethods", strings.NewReader(tt.body))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tt.method, "/redfish/v1/AggregationService/ConnectionMethods", nil)
			}
			req.Header.Set("X-Auth-Token", adminToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			if tt.checkResponse != nil {
				tt.checkResponse(t, rec)
			}
		})
	}
}

func TestHandleConnectionMethod(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	adminToken := loginAndGetToken(t, handler, "admin", "admin")

	// Create a test connection method
	method := &models.ConnectionMethod{
		ID:                   "test-cm",
		Name:                 "Test CM",
		ConnectionMethodType: "Redfish",
		Address:              "https://test.example.com",
		Username:             "admin",
		Password:             "secret",
		Enabled:              true,
	}
	if err := db.CreateConnectionMethod(context.Background(), method); err != nil {
		t.Fatalf("failed to create connection method: %v", err)
	}

	tests := []struct {
		name           string
		method         string
		id             string
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:           "GET existing connection method",
			method:         http.MethodGet,
			id:             "test-cm",
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var cm map[string]interface{}
				if err := json.Unmarshal(rec.Body.Bytes(), &cm); err != nil {
					t.Fatalf("failed to parse response: %v", err)
				}
				if cm["Id"] != "test-cm" {
					t.Errorf("unexpected Id: %v", cm["Id"])
				}
				if cm["Name"] != "Test CM" {
					t.Errorf("unexpected Name: %v", cm["Name"])
				}
			},
		},
		{
			name:           "GET non-existent connection method",
			method:         http.MethodGet,
			id:             "nonexistent",
			expectedStatus: http.StatusNotFound,
			checkResponse:  nil,
		},
		{
			name:           "OPTIONS connection method",
			method:         http.MethodOptions,
			id:             "test-cm",
			expectedStatus: http.StatusNoContent,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
				allow := rec.Header().Get("Allow")
				if allow != "GET, DELETE" {
					t.Errorf("expected Allow header 'GET, DELETE', got %q", allow)
				}
			},
		},
		{
			name:           "DELETE connection method",
			method:         http.MethodDelete,
			id:             "test-cm",
			expectedStatus: http.StatusNoContent,
			checkResponse:  nil,
		},
		{
			name:           "PUT connection method (not allowed)",
			method:         http.MethodPut,
			id:             "test-cm",
			expectedStatus: http.StatusMethodNotAllowed,
			checkResponse:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/redfish/v1/AggregationService/ConnectionMethods/" + tt.id
			req := httptest.NewRequest(tt.method, url, nil)
			req.Header.Set("X-Auth-Token", adminToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			if tt.checkResponse != nil {
				tt.checkResponse(t, rec)
			}
		})
	}
}
