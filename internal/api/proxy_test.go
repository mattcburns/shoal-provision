// Shoal is a Redfish aggregator service.
// Copyright (C) 2025 Matthew Burns
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
	"path/filepath"
	"testing"

	"shoal/internal/auth"
	"shoal/internal/bmc"
	"shoal/internal/database"
	"shoal/pkg/models"
)

func TestHandleBMCProxy(t *testing.T) {
	// Setup mock BMC server
	mockBMC := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check authentication
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("OData-Version", "4.0")

		switch r.URL.Path {
		case "/redfish/v1/Managers":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Managers",
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Managers/BMC"},
				},
			})
		case "/redfish/v1/Managers/BMC":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id":   "/redfish/v1/Managers/BMC",
				"@odata.type": "#Manager.v1_5_0.Manager",
				"Id":          "BMC",
				"Name":        "Manager",
			})
		case "/redfish/v1/Managers/BMC/LogServices":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Managers/BMC/LogServices",
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Managers/BMC/LogServices/Log1"},
				},
			})
		case "/redfish/v1/Systems":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Systems",
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Systems/System1"},
				},
			})
		case "/redfish/v1/Systems/System1":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id":   "/redfish/v1/Systems/System1",
				"@odata.type": "#ComputerSystem.v1_5_0.ComputerSystem",
				"Id":          "System1",
				"Name":        "System",
			})
		case "/redfish/v1/Systems/System1/Storage":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Systems/System1/Storage",
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Systems/System1/Storage/1"},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"code":    "Base.1.0.ResourceNotFound",
					"message": "Resource not found",
				},
			})
		}
	}))
	defer mockBMC.Close()

	// Setup test database and API handler
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	authSvc := auth.New(db)
	bmcSvc := bmc.New(db)
	handler := &Handler{
		db:     db,
		auth:   authSvc,
		bmcSvc: bmcSvc,
	}

	// Create test BMC
	testBMC := &models.BMC{
		Name:     "test-bmc",
		Address:  mockBMC.URL,
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}
	if err := db.CreateBMC(ctx, testBMC); err != nil {
		t.Fatalf("failed to create test BMC: %v", err)
	}

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		checkBody      func(*testing.T, map[string]interface{})
	}{
		{
			name:           "proxy manager root request",
			path:           "/v1/Managers/test-bmc",
			expectedStatus: http.StatusOK,
			checkBody: func(t *testing.T, body map[string]interface{}) {
				if id, ok := body["Id"].(string); !ok || id != "BMC" {
					t.Errorf("expected Id=BMC, got %v", body["Id"])
				}
				if name, ok := body["Name"].(string); !ok || name != "Manager" {
					t.Errorf("expected Name=Manager, got %v", body["Name"])
				}
			},
		},
		{
			name:           "proxy manager sub-resource",
			path:           "/v1/Managers/test-bmc/LogServices",
			expectedStatus: http.StatusOK,
			checkBody: func(t *testing.T, body map[string]interface{}) {
				members, ok := body["Members"].([]interface{})
				if !ok || len(members) == 0 {
					t.Errorf("expected Members array, got %v", body["Members"])
				}
			},
		},
		{
			name:           "proxy system root request",
			path:           "/v1/Systems/test-bmc",
			expectedStatus: http.StatusOK,
			checkBody: func(t *testing.T, body map[string]interface{}) {
				if id, ok := body["Id"].(string); !ok || id != "System1" {
					t.Errorf("expected Id=System1, got %v", body["Id"])
				}
			},
		},
		{
			name:           "proxy system sub-resource",
			path:           "/v1/Systems/test-bmc/Storage",
			expectedStatus: http.StatusOK,
			checkBody: func(t *testing.T, body map[string]interface{}) {
				members, ok := body["Members"].([]interface{})
				if !ok || len(members) == 0 {
					t.Errorf("expected Members array, got %v", body["Members"])
				}
			},
		},
		{
			name:           "proxy path too short",
			path:           "/v1/Managers",
			expectedStatus: http.StatusNotFound,
			checkBody:      nil,
		},
		{
			name:           "proxy invalid resource type",
			path:           "/v1/InvalidResource/test-bmc",
			expectedStatus: http.StatusNotFound,
			checkBody:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			handler.handleBMCProxy(rec, req, tt.path)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			if tt.checkBody != nil && rec.Code == http.StatusOK {
				var body map[string]interface{}
				if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode response body: %v", err)
				}
				tt.checkBody(t, body)
			}
		})
	}
}

func TestHandleBMCProxyWithNonexistentBMC(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	authSvc := auth.New(db)
	bmcSvc := bmc.New(db)
	handler := &Handler{
		db:     db,
		auth:   authSvc,
		bmcSvc: bmcSvc,
	}

	// Test request to non-existent BMC
	req := httptest.NewRequest(http.MethodGet, "/v1/Managers/nonexistent-bmc", nil)
	rec := httptest.NewRecorder()
	handler.handleBMCProxy(rec, req, "/v1/Managers/nonexistent-bmc")

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected status 502 for nonexistent BMC, got %d", rec.Code)
	}
}

func TestIsBMCProxyRequest(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	authSvc := auth.New(db)
	bmcSvc := bmc.New(db)
	handler := &Handler{
		db:     db,
		auth:   authSvc,
		bmcSvc: bmcSvc,
	}

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "managers root with BMC name",
			path:     "/v1/Managers/bmc1",
			expected: true,
		},
		{
			name:     "managers sub-resource",
			path:     "/v1/Managers/bmc1/LogServices",
			expected: true,
		},
		{
			name:     "systems root with BMC name",
			path:     "/v1/Systems/bmc1",
			expected: true,
		},
		{
			name:     "systems sub-resource",
			path:     "/v1/Systems/bmc1/Storage/1",
			expected: true,
		},
		{
			name:     "managers collection (no BMC name)",
			path:     "/v1/Managers",
			expected: false,
		},
		{
			name:     "systems collection (no BMC name)",
			path:     "/v1/Systems",
			expected: false,
		},
		{
			name:     "service root",
			path:     "/v1",
			expected: false,
		},
		{
			name:     "account service",
			path:     "/v1/AccountService",
			expected: false,
		},
		{
			name:     "aggregation service",
			path:     "/v1/AggregationService",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.isBMCProxyRequest(tt.path)
			if result != tt.expected {
				t.Errorf("isBMCProxyRequest(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestHandleBMCProxyHeaderPropagation(t *testing.T) {
	// Setup mock BMC server that returns specific headers
	mockBMC := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Set custom headers
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("OData-Version", "4.0")
		w.Header().Set("X-Custom-Header", "test-value")
		w.Header().Set("ETag", `"test-etag"`)

		if r.URL.Path == "/redfish/v1/Managers/BMC" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Managers/BMC",
				"Id":        "BMC",
			})
		}
	}))
	defer mockBMC.Close()

	// Setup test database and API handler
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	authSvc := auth.New(db)
	bmcSvc := bmc.New(db)
	handler := &Handler{
		db:     db,
		auth:   authSvc,
		bmcSvc: bmcSvc,
	}

	// Create test BMC
	testBMC := &models.BMC{
		Name:     "test-bmc",
		Address:  mockBMC.URL,
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}
	if err := db.CreateBMC(ctx, testBMC); err != nil {
		t.Fatalf("failed to create test BMC: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/Managers/test-bmc", nil)
	rec := httptest.NewRecorder()
	handler.handleBMCProxy(rec, req, "/v1/Managers/test-bmc")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	// Check that headers were propagated
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type=application/json, got %q", ct)
	}
	if ov := rec.Header().Get("OData-Version"); ov != "4.0" {
		t.Errorf("expected OData-Version=4.0, got %q", ov)
	}
	if custom := rec.Header().Get("X-Custom-Header"); custom != "test-value" {
		t.Errorf("expected X-Custom-Header=test-value, got %q", custom)
	}
	if etag := rec.Header().Get("ETag"); etag != `"test-etag"` {
		t.Errorf("expected ETag=\"test-etag\", got %q", etag)
	}
}
