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

package bmc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"shoal/internal/database"
	"shoal/pkg/models"
)

func TestGetFirstManagerID(t *testing.T) {
	// Create test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	// Create mock BMC server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redfish/v1/Managers" {
			// Return managers collection
			collection := map[string]interface{}{
				"@odata.id": "/redfish/v1/Managers",
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Managers/BMC"},
					{"@odata.id": "/redfish/v1/Managers/BMC2"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(collection)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	// Create test BMC
	testBMC := &models.BMC{
		Name:     "test-bmc",
		Address:  mockServer.URL,
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}
	if err := db.CreateBMC(ctx, testBMC); err != nil {
		t.Fatal(err)
	}

	// Create service
	svc := New(db)

	// Test first call (should query BMC)
	managerID, err := svc.GetFirstManagerID(ctx, "test-bmc")
	if err != nil {
		t.Fatalf("Failed to get manager ID: %v", err)
	}
	if managerID != "BMC" {
		t.Errorf("Expected manager ID 'BMC', got '%s'", managerID)
	}

	// Test cached call (should use cache)
	managerID2, err := svc.GetFirstManagerID(ctx, "test-bmc")
	if err != nil {
		t.Fatalf("Failed to get cached manager ID: %v", err)
	}
	if managerID2 != "BMC" {
		t.Errorf("Expected cached manager ID 'BMC', got '%s'", managerID2)
	}

	// Verify cache was used (check cache directly)
	svc.idCacheMux.RLock()
	cache, ok := svc.idCache["test-bmc"]
	svc.idCacheMux.RUnlock()
	if !ok {
		t.Error("Expected cache entry for test-bmc")
	}
	if cache.managerID != "BMC" {
		t.Errorf("Expected cached manager ID 'BMC', got '%s'", cache.managerID)
	}
}

func TestGetFirstSystemID(t *testing.T) {
	// Create test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	// Create mock BMC server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redfish/v1/Systems" {
			// Return systems collection
			collection := map[string]interface{}{
				"@odata.id": "/redfish/v1/Systems",
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Systems/System.Embedded.1"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(collection)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	// Create test BMC
	testBMC := &models.BMC{
		Name:     "test-bmc",
		Address:  mockServer.URL,
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}
	if err := db.CreateBMC(ctx, testBMC); err != nil {
		t.Fatal(err)
	}

	// Create service
	svc := New(db)

	// Test first call (should query BMC)
	systemID, err := svc.GetFirstSystemID(ctx, "test-bmc")
	if err != nil {
		t.Fatalf("Failed to get system ID: %v", err)
	}
	if systemID != "System.Embedded.1" {
		t.Errorf("Expected system ID 'System.Embedded.1', got '%s'", systemID)
	}

	// Test cached call (should use cache)
	systemID2, err := svc.GetFirstSystemID(ctx, "test-bmc")
	if err != nil {
		t.Fatalf("Failed to get cached system ID: %v", err)
	}
	if systemID2 != "System.Embedded.1" {
		t.Errorf("Expected cached system ID 'System.Embedded.1', got '%s'", systemID2)
	}

	// Verify cache was used
	svc.idCacheMux.RLock()
	cache, ok := svc.idCache["test-bmc"]
	svc.idCacheMux.RUnlock()
	if !ok {
		t.Error("Expected cache entry for test-bmc")
	}
	if cache.systemID != "System.Embedded.1" {
		t.Errorf("Expected cached system ID 'System.Embedded.1', got '%s'", cache.systemID)
	}
}

func TestIDCacheExpiry(t *testing.T) {
	// Create test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// Create service with cache
	svc := New(db)

	// Manually add expired cache entry
	svc.idCacheMux.Lock()
	svc.idCache["test-bmc"] = &bmcIDCache{
		managerID: "OLD_MANAGER",
		systemID:  "OLD_SYSTEM",
		cachedAt:  time.Now().Add(-10 * time.Minute), // Expired
	}
	svc.idCacheMux.Unlock()

	// Create mock BMC server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redfish/v1/Managers" {
			collection := map[string]interface{}{
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Managers/NEW_MANAGER"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(collection)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	// Create test BMC
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	testBMC := &models.BMC{
		Name:     "test-bmc",
		Address:  mockServer.URL,
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}
	if err := db.CreateBMC(ctx, testBMC); err != nil {
		t.Fatal(err)
	}

	// Get manager ID - should fetch new value because cache is expired
	managerID, err := svc.GetFirstManagerID(ctx, "test-bmc")
	if err != nil {
		t.Fatalf("Failed to get manager ID: %v", err)
	}
	if managerID != "NEW_MANAGER" {
		t.Errorf("Expected new manager ID 'NEW_MANAGER', got '%s'", managerID)
	}
}

func TestIDDiscoveryFallback(t *testing.T) {
	// Create test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	// Create mock BMC server that returns errors
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockServer.Close()

	// Create test BMC
	testBMC := &models.BMC{
		Name:     "test-bmc",
		Address:  mockServer.URL,
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}
	if err := db.CreateBMC(ctx, testBMC); err != nil {
		t.Fatal(err)
	}

	// Create service
	svc := New(db)

	// Test that discovery fails but doesn't crash
	_, err = svc.GetFirstManagerID(ctx, "test-bmc")
	if err == nil {
		t.Error("Expected error when BMC returns 500")
	}

	_, err = svc.GetFirstSystemID(ctx, "test-bmc")
	if err == nil {
		t.Error("Expected error when BMC returns 500")
	}
}
