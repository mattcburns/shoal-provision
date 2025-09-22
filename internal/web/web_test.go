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

package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shoal/internal/database"
	pkgAuth "shoal/pkg/auth"
	"shoal/pkg/models"
)

// testSetup creates a test database, user, and session for testing authenticated endpoints
type testSetup struct {
	DB      *database.DB
	Handler http.Handler
	User    *models.User
	Session *models.Session
}

func createTestSetup(t *testing.T) *testSetup {
	// Create a test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Create a test user for authentication
	passwordHash, _ := pkgAuth.HashPassword("admin")
	testUser := &models.User{
		ID:           "test-user",
		Username:     "admin",
		PasswordHash: passwordHash,
		Role:         "admin",
		Enabled:      true,
	}
	if err := db.CreateUser(ctx, testUser); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create a session for the test user
	testSession := &models.Session{
		ID:        "test-session",
		Token:     "test-token",
		UserID:    testUser.ID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := db.CreateSession(ctx, testSession); err != nil {
		t.Fatalf("Failed to create test session: %v", err)
	}

	// Create handler
	handler := New(db)

	return &testSetup{
		DB:      db,
		Handler: handler,
		User:    testUser,
		Session: testSession,
	}
}

// addAuth adds authentication cookie to a request
func (ts *testSetup) addAuth(req *http.Request) {
	req.AddCookie(&http.Cookie{
		Name:  "session_token",
		Value: ts.Session.Token,
	})
}

func TestHandleEditBMC(t *testing.T) {
	ts := createTestSetup(t)
	defer ts.DB.Close()

	ctx := context.Background()

	// Create a test BMC first
	testBMC := &models.BMC{
		Name:        "test-bmc",
		Address:     "192.168.1.100",
		Username:    "admin",
		Password:    "password",
		Description: "Test BMC",
		Enabled:     true,
	}
	if err := ts.DB.CreateBMC(ctx, testBMC); err != nil {
		t.Fatalf("Failed to create test BMC: %v", err)
	}

	t.Run("GET Edit Form", func(t *testing.T) {
		// Create request to get edit form
		req := httptest.NewRequest("GET", fmt.Sprintf("/bmcs/edit?id=%d", testBMC.ID), nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		// Check response
		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		// Check that the form contains the BMC data
		body := w.Body.String()
		if !strings.Contains(body, testBMC.Name) {
			t.Error("Response should contain BMC name")
		}
		if !strings.Contains(body, testBMC.Address) {
			t.Error("Response should contain BMC address")
		}
		if !strings.Contains(body, testBMC.Username) {
			t.Error("Response should contain BMC username")
		}
		if !strings.Contains(body, "Edit BMC") {
			t.Error("Response should contain Edit BMC title")
		}
	})

	t.Run("POST Edit BMC", func(t *testing.T) {
		// Create form data
		form := url.Values{}
		form.Add("name", "updated-bmc")
		form.Add("address", "192.168.1.101")
		form.Add("username", "newadmin")
		form.Add("password", "newpassword")
		form.Add("description", "Updated description")
		form.Add("enabled", "on")

		// Create POST request
		req := httptest.NewRequest("POST", fmt.Sprintf("/bmcs/edit?id=%d", testBMC.ID), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		// Check redirect
		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status %d, got %d", http.StatusSeeOther, w.Code)
		}

		// Check that BMC was updated in database
		updatedBMC, err := ts.DB.GetBMC(ctx, testBMC.ID)
		if err != nil {
			t.Fatalf("Failed to get updated BMC: %v", err)
		}

		if updatedBMC.Name != "updated-bmc" {
			t.Errorf("Expected name 'updated-bmc', got '%s'", updatedBMC.Name)
		}
		if updatedBMC.Address != "192.168.1.101" {
			t.Errorf("Expected address '192.168.1.101', got '%s'", updatedBMC.Address)
		}
		if updatedBMC.Username != "newadmin" {
			t.Errorf("Expected username 'newadmin', got '%s'", updatedBMC.Username)
		}
		if updatedBMC.Description != "Updated description" {
			t.Errorf("Expected description 'Updated description', got '%s'", updatedBMC.Description)
		}
	})

	t.Run("Edit Non-existent BMC", func(t *testing.T) {
		// Try to edit a BMC that doesn't exist
		req := httptest.NewRequest("GET", "/bmcs/edit?id=9999", nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		// Should redirect with error
		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status %d, got %d", http.StatusSeeOther, w.Code)
		}

		location := w.Header().Get("Location")
		if !strings.Contains(location, "error") {
			t.Error("Should redirect with error for non-existent BMC")
		}
	})

	t.Run("Edit with Missing ID", func(t *testing.T) {
		// Try to edit without providing ID
		req := httptest.NewRequest("GET", "/bmcs/edit", nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		// Should redirect with error
		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status %d, got %d", http.StatusSeeOther, w.Code)
		}

		location := w.Header().Get("Location")
		if !strings.Contains(location, "error") {
			t.Error("Should redirect with error for missing ID")
		}
	})

	t.Run("Edit with Invalid Data", func(t *testing.T) {
		// Create form data with missing required fields
		form := url.Values{}
		form.Add("name", "") // Empty name
		form.Add("address", "192.168.1.101")
		form.Add("username", "admin")
		form.Add("password", "password")

		// Create POST request
		req := httptest.NewRequest("POST", fmt.Sprintf("/bmcs/edit?id=%d", testBMC.ID), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		// Should redirect with error
		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status %d, got %d", http.StatusSeeOther, w.Code)
		}

		location := w.Header().Get("Location")
		if !strings.Contains(location, "error") {
			t.Error("Should redirect with error for invalid data")
		}
	})
}

func TestHandleAddBMC(t *testing.T) {
	ts := createTestSetup(t)
	defer ts.DB.Close()

	ctx := context.Background()

	t.Run("GET Add Form", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/bmcs/add", nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Add New BMC") {
			t.Error("Response should contain Add New BMC title")
		}
	})

	t.Run("POST Add BMC", func(t *testing.T) {
		form := url.Values{}
		form.Add("name", "new-bmc")
		form.Add("address", "192.168.1.200")
		form.Add("username", "admin")
		form.Add("password", "password")
		form.Add("description", "New BMC")
		form.Add("enabled", "on")

		req := httptest.NewRequest("POST", "/bmcs/add", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status %d, got %d", http.StatusSeeOther, w.Code)
		}

		// Check that BMC was created
		bmcs, err := ts.DB.GetBMCs(ctx)
		if err != nil {
			t.Fatalf("Failed to get BMCs: %v", err)
		}

		found := false
		for _, bmc := range bmcs {
			if bmc.Name == "new-bmc" {
				found = true
				break
			}
		}
		if !found {
			t.Error("BMC was not created")
		}
	})
}

func TestHandleDeleteBMC(t *testing.T) {
	ts := createTestSetup(t)
	defer ts.DB.Close()

	ctx := context.Background()

	// Create a test BMC
	testBMC := &models.BMC{
		Name:     "delete-test-bmc",
		Address:  "192.168.1.100",
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}
	if err := ts.DB.CreateBMC(ctx, testBMC); err != nil {
		t.Fatalf("Failed to create test BMC: %v", err)
	}

	t.Run("Delete BMC", func(t *testing.T) {
		req := httptest.NewRequest("GET", fmt.Sprintf("/bmcs/delete?id=%d", testBMC.ID), nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status %d, got %d", http.StatusSeeOther, w.Code)
		}

		// Check that BMC was deleted
		deletedBMC, err := ts.DB.GetBMC(ctx, testBMC.ID)
		if err != nil {
			t.Fatalf("Failed to check deleted BMC: %v", err)
		}
		if deletedBMC != nil {
			t.Error("BMC should have been deleted")
		}
	})

	t.Run("Delete with Missing ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/bmcs/delete", nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status %d, got %d", http.StatusSeeOther, w.Code)
		}

		location := w.Header().Get("Location")
		if !strings.Contains(location, "error") {
			t.Error("Should redirect with error for missing ID")
		}
	})
}

func TestHandleHome(t *testing.T) {
	ts := createTestSetup(t)
	defer ts.DB.Close()

	t.Run("Dashboard", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Dashboard") {
			t.Error("Response should contain Dashboard title")
		}
		if !strings.Contains(body, "BMC Status Overview") {
			t.Error("Response should contain BMC Status Overview")
		}
	})
}

func TestHandleBMCs(t *testing.T) {
	ts := createTestSetup(t)
	defer ts.DB.Close()

	ctx := context.Background()

	// Create test BMCs
	bmc1 := &models.BMC{
		Name:     "bmc1",
		Address:  "192.168.1.100",
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}
	bmc2 := &models.BMC{
		Name:     "bmc2",
		Address:  "192.168.1.101",
		Username: "admin",
		Password: "password",
		Enabled:  false,
	}

	if err := ts.DB.CreateBMC(ctx, bmc1); err != nil {
		t.Fatalf("Failed to create BMC1: %v", err)
	}
	if err := ts.DB.CreateBMC(ctx, bmc2); err != nil {
		t.Fatalf("Failed to create BMC2: %v", err)
	}

	t.Run("BMC Management Page", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/bmcs", nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Manage BMCs") {
			t.Error("Response should contain Manage BMCs title")
		}
		if !strings.Contains(body, "bmc1") {
			t.Error("Response should contain bmc1")
		}
		if !strings.Contains(body, "bmc2") {
			t.Error("Response should contain bmc2")
		}
		if !strings.Contains(body, "192.168.1.100") {
			t.Error("Response should contain BMC1 address")
		}
		if !strings.Contains(body, "192.168.1.101") {
			t.Error("Response should contain BMC2 address")
		}
		// Check for Edit buttons
		if !strings.Contains(body, "Edit") {
			t.Error("Response should contain Edit buttons")
		}
	})

	t.Run("BMC Management with Message", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/bmcs?message=Test+message", nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Test message") {
			t.Error("Response should contain the message")
		}
	})

	t.Run("BMC Management with Error", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/bmcs?error=Test+error", nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Test error") {
			t.Error("Response should contain the error")
		}
	})
}

func TestHandleBMCDetails(t *testing.T) {
	ts := createTestSetup(t)
	defer ts.DB.Close()

	ctx := context.Background()

	// Create a test BMC
	testBMC := &models.BMC{
		Name:     "test-details-bmc",
		Address:  "192.168.1.100",
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}
	if err := ts.DB.CreateBMC(ctx, testBMC); err != nil {
		t.Fatalf("Failed to create test BMC: %v", err)
	}

	t.Run("GET BMC Details Page", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/bmcs/details?name=test-details-bmc", nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "BMC Details - BMC Details - test-details-bmc") {
			t.Error("Response should contain BMC details title")
		}
		if !strings.Contains(body, "Back to BMC List") {
			t.Error("Response should contain back link")
		}
		if !strings.Contains(body, "System Information") {
			t.Error("Response should contain System Information section")
		}
		if !strings.Contains(body, "Network Interfaces") {
			t.Error("Response should contain Network Interfaces section")
		}
		if !strings.Contains(body, "Storage Devices") {
			t.Error("Response should contain Storage Devices section")
		}
		if !strings.Contains(body, "System Event Log") {
			t.Error("Response should contain System Event Log section")
		}
		if !strings.Contains(body, "loadBMCDetails()") {
			t.Error("Response should contain JavaScript to load BMC details")
		}
	})

	t.Run("BMC Details with Missing Name", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/bmcs/details", nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		// Should redirect with error
		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status %d, got %d", http.StatusSeeOther, w.Code)
		}

		location := w.Header().Get("Location")
		if !strings.Contains(location, "error") {
			t.Error("Should redirect with error for missing BMC name")
		}
	})

	t.Run("BMC Details with Error Parameter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/bmcs/details?name=test-bmc&error=Test+error", nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Test error") {
			t.Error("Response should contain the error message")
		}
	})
}

func TestHandleBMCDetailsAPI(t *testing.T) {
	ts := createTestSetup(t)
	defer ts.DB.Close()

	t.Run("API Details with Missing Name", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/bmcs/details", nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
		}

		// Check for JSON error response
		if w.Header().Get("Content-Type") != "application/json" {
			t.Error("Response should have JSON content type")
		}

		body := w.Body.String()
		if !strings.Contains(body, "BMC name is required") {
			t.Error("Response should contain error message")
		}
	})

	t.Run("API Details with Non-existent BMC", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/bmcs/details?name=non-existent", nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
		}

		// Check for JSON error response
		if w.Header().Get("Content-Type") != "application/json" {
			t.Error("Response should have JSON content type")
		}

		body := w.Body.String()
		if !strings.Contains(body, "Failed to get BMC details") {
			t.Error("Response should contain error message")
		}
	})

	t.Run("API Details POST Method Not Allowed", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/bmcs/details?name=test", nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()

		ts.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, w.Code)
		}
	})

	// Test successful API call would require a mock BMC server
	// This is more complex and would be similar to the BMC service tests
	// For now, we test the error cases and basic validation
}

func TestBMCSettingsAPI(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Create an admin user and a session cookie helper
	passwordHash, _ := pkgAuth.HashPassword("admin")
	admin := &models.User{ID: "u1", Username: "admin", PasswordHash: passwordHash, Role: models.RoleAdmin, Enabled: true}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Mock BMC server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/redfish/v1/Systems":
			json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/S1"}}})
		case "/redfish/v1/Systems/S1/Bios":
			json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Systems/S1/Bios/Settings"}},
				"Attributes":        map[string]any{"LogicalProc": true},
			})
		case "/redfish/v1/Managers":
			json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/M1"}}})
		case "/redfish/v1/Managers/M1/NetworkProtocol":
			json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Managers/M1/NetworkProtocol/Settings"}},
				"HTTPS":             map[string]any{"Port": float64(443)},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	b := &models.BMC{Name: "b1", Address: server.URL, Username: "admin", Password: "password", Enabled: true}
	if err := db.CreateBMC(ctx, b); err != nil {
		t.Fatalf("failed to create bmc: %v", err)
	}

	handler := New(db)

	// Authenticate via basic auth
	req := httptest.NewRequest(http.MethodGet, "/api/bmcs/b1/settings", nil)
	req.SetBasicAuth("admin", "admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Descriptors []models.SettingDescriptor `json:"descriptors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(resp.Descriptors) == 0 {
		t.Fatalf("expected descriptors > 0")
	}
}

func TestBMCSettingsDetailAPI(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Admin user for basic auth
	passwordHash, _ := pkgAuth.HashPassword("admin")
	admin := &models.User{ID: "u1", Username: "admin", PasswordHash: passwordHash, Role: models.RoleAdmin, Enabled: true}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Mock BMC server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/redfish/v1/Systems":
			json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/S1"}}})
		case "/redfish/v1/Systems/S1/Bios":
			json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Systems/S1/Bios/Settings"}},
				"Attributes":        map[string]any{"LogicalProc": true},
			})
		case "/redfish/v1/Managers":
			json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/M1"}}})
		case "/redfish/v1/Managers/M1/NetworkProtocol":
			json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Managers/M1/NetworkProtocol/Settings"}},
				"HTTPS":             map[string]any{"Port": float64(443)},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Seed BMC
	b := &models.BMC{Name: "b1", Address: server.URL, Username: "admin", Password: "password", Enabled: true}
	if err := db.CreateBMC(ctx, b); err != nil {
		t.Fatalf("failed to create bmc: %v", err)
	}

	handler := New(db)

	// First list to discover and persist
	listReq := httptest.NewRequest(http.MethodGet, "/api/bmcs/b1/settings", nil)
	listReq.SetBasicAuth("admin", "admin")
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list expected 200, got %d: %s", listRec.Code, listRec.Body.String())
	}
	var listResp struct{ Descriptors []models.SettingDescriptor }
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("parse list: %v", err)
	}
	if len(listResp.Descriptors) == 0 {
		t.Fatalf("expected descriptors")
	}
	id := listResp.Descriptors[0].ID

	// Now detail
	detReq := httptest.NewRequest(http.MethodGet, "/api/bmcs/b1/settings/"+id, nil)
	detReq.SetBasicAuth("admin", "admin")
	detRec := httptest.NewRecorder()
	handler.ServeHTTP(detRec, detReq)
	if detRec.Code != http.StatusOK {
		t.Fatalf("detail expected 200, got %d: %s", detRec.Code, detRec.Body.String())
	}
	var desc models.SettingDescriptor
	if err := json.Unmarshal(detRec.Body.Bytes(), &desc); err != nil {
		t.Fatalf("parse detail: %v", err)
	}
	if desc.ID != id {
		t.Fatalf("expected id %s, got %s", id, desc.ID)
	}
}

func TestProfilesAPI(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Admin for basic auth
	passwordHash, _ := pkgAuth.HashPassword("admin")
	admin := &models.User{ID: "u1", Username: "admin", PasswordHash: passwordHash, Role: models.RoleAdmin, Enabled: true}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	handler := New(db)

	// Create profile
	pr := models.Profile{Name: "Baseline", Description: "desc"}
	body, _ := json.Marshal(pr)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles", bytes.NewReader(body))
	req.SetBasicAuth("admin", "admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create profile expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created models.Profile
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("missing profile id")
	}

	// List profiles
	req = httptest.NewRequest(http.MethodGet, "/api/profiles", nil)
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list profiles expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var plist []models.Profile
	if err := json.Unmarshal(rec.Body.Bytes(), &plist); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(plist) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(plist))
	}

	// Get profile
	req = httptest.NewRequest(http.MethodGet, "/api/profiles/"+created.ID, nil)
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get profile expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got models.Profile
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("id mismatch")
	}

	// Create version
	v := models.ProfileVersion{Notes: "v1", Entries: []models.ProfileEntry{{ResourcePath: "/redfish/v1/Managers/M1/NetworkProtocol", Attribute: "HTTPS.Port", DesiredValue: 443}}}
	body, _ = json.Marshal(v)
	req = httptest.NewRequest(http.MethodPost, "/api/profiles/"+created.ID+"/versions", bytes.NewReader(body))
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create version expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var vcreated models.ProfileVersion
	if err := json.Unmarshal(rec.Body.Bytes(), &vcreated); err != nil {
		t.Fatalf("decode version: %v", err)
	}
	if vcreated.Version != 1 {
		t.Fatalf("expected version 1, got %d", vcreated.Version)
	}

	// List versions
	req = httptest.NewRequest(http.MethodGet, "/api/profiles/"+created.ID+"/versions", nil)
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list versions expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var vlist []models.ProfileVersion
	if err := json.Unmarshal(rec.Body.Bytes(), &vlist); err != nil {
		t.Fatalf("decode vlist: %v", err)
	}
	if len(vlist) != 1 {
		t.Fatalf("expected 1 version, got %d", len(vlist))
	}

	// Get version
	req = httptest.NewRequest(http.MethodGet, "/api/profiles/"+created.ID+"/versions/1", nil)
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get version expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var vgot models.ProfileVersion
	if err := json.Unmarshal(rec.Body.Bytes(), &vgot); err != nil {
		t.Fatalf("decode vget: %v", err)
	}
	if vgot.Version != 1 {
		t.Fatalf("wrong version")
	}

	// Create assignment
	a := models.ProfileAssignment{TargetType: "bmc", TargetValue: "b1", Version: 1}
	body, _ = json.Marshal(a)
	req = httptest.NewRequest(http.MethodPost, "/api/profiles/"+created.ID+"/assignments", bytes.NewReader(body))
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create assignment expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var acreated models.ProfileAssignment
	if err := json.Unmarshal(rec.Body.Bytes(), &acreated); err != nil {
		t.Fatalf("decode assign: %v", err)
	}
	if acreated.ID == "" {
		t.Fatalf("missing assignment id")
	}

	// List assignments
	req = httptest.NewRequest(http.MethodGet, "/api/profiles/"+created.ID+"/assignments", nil)
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list assignments expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var alist []models.ProfileAssignment
	if err := json.Unmarshal(rec.Body.Bytes(), &alist); err != nil {
		t.Fatalf("decode alist: %v", err)
	}
	if len(alist) != 1 {
		t.Fatalf("expected 1 assignment, got %d", len(alist))
	}

	// Delete assignment
	req = httptest.NewRequest(http.MethodDelete, "/api/profiles/"+created.ID+"/assignments/"+alist[0].ID, nil)
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete assignment expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Delete profile
	req = httptest.NewRequest(http.MethodDelete, "/api/profiles/"+created.ID, nil)
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete profile expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestProfilesPreviewAPI(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("db new: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Admin user
	passwordHash, _ := pkgAuth.HashPassword("admin")
	admin := &models.User{ID: "u1", Username: "admin", PasswordHash: passwordHash, Role: models.RoleAdmin, Enabled: true}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Mock BMC redfish server that exposes settings endpoints used by discovery
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/redfish/v1/Systems":
			json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/S1"}}})
		case "/redfish/v1/Systems/S1/Bios":
			json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Systems/S1/Bios/Settings"}},
				"Attributes":        map[string]any{"LogicalProc": true},
			})
		case "/redfish/v1/Managers":
			json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/M1"}}})
		case "/redfish/v1/Managers/M1/NetworkProtocol":
			json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Managers/M1/NetworkProtocol/Settings"}},
				"HTTPS":             map[string]any{"Port": float64(443)},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Seed BMC with credentials matching server basic auth
	b := &models.BMC{Name: "b1", Address: server.URL, Username: "admin", Password: "password", Enabled: true}
	if err := db.CreateBMC(ctx, b); err != nil {
		t.Fatalf("create bmc: %v", err)
	}

	handler := New(db)

	// Discover settings (list endpoint) to persist current value snapshot
	req := httptest.NewRequest(http.MethodGet, "/api/bmcs/b1/settings", nil)
	req.SetBasicAuth("admin", "admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings list expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Create a profile whose desired value differs from current (HTTPS.Port currently 443, set 444)
	p := models.Profile{Name: "baseline"}
	body, _ := json.Marshal(p)
	req = httptest.NewRequest(http.MethodPost, "/api/profiles", bytes.NewReader(body))
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create profile: %d %s", rec.Code, rec.Body.String())
	}
	var created models.Profile
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Create version with an entry that will produce a change and another that is same
	v := models.ProfileVersion{Notes: "v1",
		Entries: []models.ProfileEntry{
			{ResourcePath: "/redfish/v1/Managers/M1/NetworkProtocol", Attribute: "HTTPS.Port", DesiredValue: 444},  // change
			{ResourcePath: "/redfish/v1/Systems/S1/Bios", Attribute: "Attributes.LogicalProc", DesiredValue: true}, // same
		},
	}
	body, _ = json.Marshal(v)
	req = httptest.NewRequest(http.MethodPost, "/api/profiles/"+created.ID+"/versions", bytes.NewReader(body))
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create version: %d %s", rec.Code, rec.Body.String())
	}

	// Preview
	req = httptest.NewRequest(http.MethodGet, "/api/profiles/"+created.ID+"/preview?bmc=b1", nil)
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var preview struct {
		Changes   []map[string]any `json:"changes"`
		Same      []map[string]any `json:"same"`
		Unmatched []map[string]any `json:"unmatched"`
		Summary   struct{ Total, Changes, Same, Unmatched int }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if preview.Summary.Total != 2 {
		t.Fatalf("expected total 2, got %d", preview.Summary.Total)
	}
	if preview.Summary.Changes != 1 || len(preview.Changes) != 1 {
		t.Fatalf("expected 1 change, got %+v", preview.Summary)
	}
	if preview.Summary.Same != 1 || len(preview.Same) != 1 {
		t.Fatalf("expected 1 same, got %+v", preview.Summary)
	}
}

func TestProfilesImportExportAPI(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("db new: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Admin user
	passwordHash, _ := pkgAuth.HashPassword("admin")
	admin := &models.User{ID: "u1", Username: "admin", PasswordHash: passwordHash, Role: models.RoleAdmin, Enabled: true}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatalf("create user: %v", err)
	}

	handler := New(db)

	// Create a profile and a version
	p := models.Profile{Name: "export-src", Description: "d"}
	body, _ := json.Marshal(p)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles", bytes.NewReader(body))
	req.SetBasicAuth("admin", "admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create profile: %d %s", rec.Code, rec.Body.String())
	}
	var created models.Profile
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	v := models.ProfileVersion{Notes: "v1", Entries: []models.ProfileEntry{{ResourcePath: "/redfish/v1/Managers/M1/NetworkProtocol", Attribute: "HTTPS.Port", DesiredValue: 444}}}
	body, _ = json.Marshal(v)
	req = httptest.NewRequest(http.MethodPost, "/api/profiles/"+created.ID+"/versions", bytes.NewReader(body))
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create version: %d %s", rec.Code, rec.Body.String())
	}

	// Export latest
	req = httptest.NewRequest(http.MethodPost, "/api/profiles/"+created.ID+"/export", bytes.NewReader([]byte(`{}`)))
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("export expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var exported struct {
		Profile  models.Profile
		Versions []models.ProfileVersion
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &exported); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if exported.Profile.ID != created.ID || len(exported.Versions) != 1 {
		t.Fatalf("unexpected export content")
	}

	// Import as new profile: clear IDs and change name
	exported.Profile.ID = ""
	exported.Profile.Name = "import-dest"
	for i := range exported.Versions {
		exported.Versions[i].ID = ""
		exported.Versions[i].ProfileID = ""
		// keep version numbers
		for j := range exported.Versions[i].Entries {
			exported.Versions[i].Entries[j].ID = ""
			exported.Versions[i].Entries[j].ProfileVersionID = ""
		}
	}
	body, _ = json.Marshal(exported)
	req = httptest.NewRequest(http.MethodPost, "/api/profiles/import", bytes.NewReader(body))
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify new profile exists
	req = httptest.NewRequest(http.MethodGet, "/api/profiles", nil)
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list expected 200, got %d", rec.Code)
	}
	var ps []models.Profile
	_ = json.Unmarshal(rec.Body.Bytes(), &ps)
	found := false
	for _, pp := range ps {
		if pp.Name == "import-dest" {
			found = true
		}
	}
	if !found {
		t.Fatalf("imported profile not found")
	}
}

func TestProfilesSnapshotAPI(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("db new: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Admin user
	passwordHash, _ := pkgAuth.HashPassword("admin")
	admin := &models.User{ID: "u1", Username: "admin", PasswordHash: passwordHash, Role: models.RoleAdmin, Enabled: true}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Mock BMC Redfish server with BIOS and HTTPS.Port
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/redfish/v1/Systems":
			json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/S1"}}})
		case "/redfish/v1/Systems/S1/Bios":
			json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Systems/S1/Bios/Settings"}},
				"Attributes":        map[string]any{"LogicalProc": true},
			})
		case "/redfish/v1/Managers":
			json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/M1"}}})
		case "/redfish/v1/Managers/M1/NetworkProtocol":
			json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Managers/M1/NetworkProtocol/Settings"}},
				"HTTPS":             map[string]any{"Port": float64(443)},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Seed BMC
	b := &models.BMC{Name: "b1", Address: server.URL, Username: "admin", Password: "password", Enabled: true}
	if err := db.CreateBMC(ctx, b); err != nil {
		t.Fatalf("create bmc: %v", err)
	}

	handler := New(db)

	// Create profile via snapshot
	snapReqBody := map[string]any{"name": "snap1", "description": "baseline"}
	sb, _ := json.Marshal(snapReqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/snapshot?bmc=b1", bytes.NewReader(sb))
	req.SetBasicAuth("admin", "admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("snapshot expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Profile models.Profile
		Version models.ProfileVersion
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if out.Profile.ID == "" || out.Version.Version != 1 {
		t.Fatalf("invalid snapshot result")
	}
	if len(out.Version.Entries) == 0 {
		t.Fatalf("expected entries in snapshot")
	}

	// Spot-check entries include flattened keys and correct values
	hasHTTPS := false
	hasLogical := false
	for _, e := range out.Version.Entries {
		if e.ResourcePath == "/redfish/v1/Managers/M1/NetworkProtocol" && e.Attribute == "HTTPS.Port" {
			if v, ok := e.DesiredValue.(float64); !ok || int(v) != 443 {
				t.Fatalf("expected HTTPS.Port 443")
			}
			hasHTTPS = true
		}
		if e.ResourcePath == "/redfish/v1/Systems/S1/Bios" && e.Attribute == "Attributes.LogicalProc" {
			if vb, ok := e.DesiredValue.(bool); !ok || vb != true {
				t.Fatalf("expected Attributes.LogicalProc true")
			}
			hasLogical = true
		}
	}
	if !hasHTTPS || !hasLogical {
		t.Fatalf("snapshot entries missing expected keys")
	}
}

func TestProfilesDiffAPI(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("db new: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Admin user
	passwordHash, _ := pkgAuth.HashPassword("admin")
	admin := &models.User{ID: "u1", Username: "admin", PasswordHash: passwordHash, Role: models.RoleAdmin, Enabled: true}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatalf("create user: %v", err)
	}

	handler := New(db)

	// Create a profile and two versions with differing entries
	p := models.Profile{Name: "diff-p"}
	body, _ := json.Marshal(p)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles", bytes.NewReader(body))
	req.SetBasicAuth("admin", "admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create profile: %d %s", rec.Code, rec.Body.String())
	}
	var created models.Profile
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	v1 := models.ProfileVersion{Notes: "v1", Entries: []models.ProfileEntry{
		{ResourcePath: "/redfish/v1/Managers/M1/NetworkProtocol", Attribute: "HTTPS.Port", DesiredValue: 443},
		{ResourcePath: "/redfish/v1/Systems/S1/Bios", Attribute: "Attributes.LogicalProc", DesiredValue: true},
	}}
	body, _ = json.Marshal(v1)
	req = httptest.NewRequest(http.MethodPost, "/api/profiles/"+created.ID+"/versions", bytes.NewReader(body))
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create v1: %d %s", rec.Code, rec.Body.String())
	}

	v2 := models.ProfileVersion{Notes: "v2", Entries: []models.ProfileEntry{
		{ResourcePath: "/redfish/v1/Managers/M1/NetworkProtocol", Attribute: "HTTPS.Port", DesiredValue: 444},     // changed
		{ResourcePath: "/redfish/v1/Managers/M1/NetworkProtocol", Attribute: "HTTPS.Enabled", DesiredValue: true}, // added
		// LogicalProc removed
	}}
	body, _ = json.Marshal(v2)
	req = httptest.NewRequest(http.MethodPost, "/api/profiles/"+created.ID+"/versions", bytes.NewReader(body))
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create v2: %d %s", rec.Code, rec.Body.String())
	}

	// Diff v1 -> v2
	diffReq := map[string]any{"left": map[string]any{"profile_id": created.ID, "version": 1}, "right": map[string]any{"profile_id": created.ID, "version": 2}}
	body, _ = json.Marshal(diffReq)
	req = httptest.NewRequest(http.MethodPost, "/api/profiles/diff", bytes.NewReader(body))
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("diff expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var diff struct {
		Added   []map[string]any
		Removed []map[string]any
		Changed []map[string]any
		Summary struct{ Added, Removed, Changed int }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &diff); err != nil {
		t.Fatalf("decode diff: %v", err)
	}
	if diff.Summary.Added != 1 || diff.Summary.Removed != 1 || diff.Summary.Changed != 1 {
		t.Fatalf("unexpected diff summary: %+v", diff.Summary)
	}
}
