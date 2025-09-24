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
	defer func() { _ = ts.DB.Close() }()

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
	defer func() { _ = ts.DB.Close() }()

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
	defer func() { _ = ts.DB.Close() }()

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
	defer func() { _ = ts.DB.Close() }()

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
	defer func() { _ = ts.DB.Close() }()

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
	defer func() { _ = ts.DB.Close() }()

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
		// Drag-and-drop Boot Order list should be present
		if !strings.Contains(body, "id=\"boot-order-list\"") {
			t.Error("Boot Order list should be present")
		}
		if !strings.Contains(body, "role=\"listbox\"") {
			t.Error("Boot Order list should advertise role listbox for accessibility")
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
	defer func() { _ = ts.DB.Close() }()

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
	defer func() { _ = db.Close() }()

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
			_ = json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/S1"}}})
		case "/redfish/v1/Systems/S1/Bios":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Systems/S1/Bios/Settings"}},
				"Attributes":        map[string]any{"LogicalProc": true},
			})
		case "/redfish/v1/Managers":
			_ = json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/M1"}}})
		case "/redfish/v1/Managers/M1/NetworkProtocol":
			_ = json.NewEncoder(w).Encode(map[string]any{
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
	defer func() { _ = db.Close() }()

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
			_ = json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/S1"}}})
		case "/redfish/v1/Systems/S1/Bios":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Systems/S1/Bios/Settings"}},
				"Attributes":        map[string]any{"LogicalProc": true},
			})
		case "/redfish/v1/Managers":
			_ = json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/M1"}}})
		case "/redfish/v1/Managers/M1/NetworkProtocol":
			_ = json.NewEncoder(w).Encode(map[string]any{
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

func TestBMCSettingsPaginationAndSearch(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("db new: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Admin
	passwordHash, _ := pkgAuth.HashPassword("admin")
	admin := &models.User{ID: "u1", Username: "admin", PasswordHash: passwordHash, Role: models.RoleAdmin, Enabled: true}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Mock BMC exposing multiple settings
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/redfish/v1/Systems":
			_ = json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/S1"}}})
		case "/redfish/v1/Systems/S1/Bios":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Systems/S1/Bios/Settings"}},
				"Attributes":        map[string]any{"LogicalProc": true, "Virtualization": true},
			})
		case "/redfish/v1/Managers":
			_ = json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/M1"}}})
		case "/redfish/v1/Managers/M1/NetworkProtocol":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Managers/M1/NetworkProtocol/Settings"}},
				"HTTPS":             map[string]any{"Port": float64(443)},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// BMC
	b := &models.BMC{Name: "b1", Address: server.URL, Username: "admin", Password: "password", Enabled: true}
	if err := db.CreateBMC(ctx, b); err != nil {
		t.Fatalf("create bmc: %v", err)
	}

	handler := New(db)

	// Fetch with small page size
	req := httptest.NewRequest(http.MethodGet, "/api/bmcs/b1/settings?page_size=1", nil)
	req.SetBasicAuth("admin", "admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("page1 expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp1 struct {
		Descriptors []models.SettingDescriptor `json:"descriptors"`
		Total       int                        `json:"total"`
		Page        int                        `json:"page"`
		PageSize    int                        `json:"page_size"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp1.Page != 1 || resp1.PageSize != 1 {
		t.Fatalf("pagination metadata wrong: %+v", resp1)
	}
	if len(resp1.Descriptors) != 1 {
		t.Fatalf("expected 1 descriptor on page, got %d", len(resp1.Descriptors))
	}
	if resp1.Total < 2 {
		t.Fatalf("expected total >= 2, got %d", resp1.Total)
	}

	// Next page
	req = httptest.NewRequest(http.MethodGet, "/api/bmcs/b1/settings?page=2&page_size=1", nil)
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("page2 expected 200, got %d", rec.Code)
	}
	var resp2 struct {
		Descriptors []models.SettingDescriptor `json:"descriptors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("decode2: %v", err)
	}
	if len(resp2.Descriptors) != 1 {
		t.Fatalf("expected 1 descriptor on page2, got %d", len(resp2.Descriptors))
	}

	// Search filter should match BIOS attribute by name
	req = httptest.NewRequest(http.MethodGet, "/api/bmcs/b1/settings?search=logicalproc", nil)
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search expected 200, got %d", rec.Code)
	}
	var respSearch struct {
		Descriptors []models.SettingDescriptor `json:"descriptors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &respSearch); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(respSearch.Descriptors) == 0 {
		t.Fatalf("expected search to return results")
	}
}

// Profiles API removed in Design 014
func TestProfilesAPI(t *testing.T) {}

// Profiles Preview removed in Design 014
func TestProfilesPreviewAPI(t *testing.T) {}

// Boot order apply via Profiles removed in Design 014; new endpoints will be tested in 015
func TestBootOrderPreviewAndApply(t *testing.T) {}

// Profiles Apply DryRun removed in Design 014
func TestProfilesApplyDryRunAPI(t *testing.T) {}

// Profiles Import/Export removed in Design 014
func TestProfilesImportExportAPI(t *testing.T) {}

// Profiles Snapshot removed in Design 014
func TestProfilesSnapshotAPI(t *testing.T) {}

// Profiles Diff removed in Design 014
func TestProfilesDiffAPI(t *testing.T) {}
