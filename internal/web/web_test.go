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
	"io"
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

func TestProfilesUI_ReadOnlyPages(t *testing.T) {
	ts := createTestSetup(t)
	defer func() { _ = ts.DB.Close() }()

	ctx := context.Background()

	// Seed: one profile with two versions and a couple entries
	prof := &models.Profile{
		Name:        "Baseline",
		Description: "Golden settings",
		CreatedBy:   "tester",
	}
	if err := ts.DB.CreateProfile(ctx, prof); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	v1 := &models.ProfileVersion{ProfileID: prof.ID, Version: 1, Notes: "initial", Entries: []models.ProfileEntry{
		{ResourcePath: "/redfish/v1/Systems/1/EthernetInterfaces/1", Attribute: "MACAddress", DesiredValue: "AA:BB:CC:00:11:22"},
		{ResourcePath: "/redfish/v1/Systems/1", Attribute: "AssetTag", DesiredValue: "ASSET-001"},
	}}
	if err := ts.DB.CreateProfileVersion(ctx, v1); err != nil {
		t.Fatalf("CreateProfileVersion v1: %v", err)
	}
	v2 := &models.ProfileVersion{ProfileID: prof.ID, Version: 2, Notes: "update tag", Entries: []models.ProfileEntry{
		{ResourcePath: "/redfish/v1/Systems/1", Attribute: "AssetTag", DesiredValue: "ASSET-002"},
	}}
	if err := ts.DB.CreateProfileVersion(ctx, v2); err != nil {
		t.Fatalf("CreateProfileVersion v2: %v", err)
	}

	t.Run("List Page", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/profiles", nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()
		ts.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("/profiles status got %d", w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, "Configuration Profiles") {
			t.Errorf("expected header present")
		}
		// CRUD: New Profile button present
		if !strings.Contains(body, "New Profile") {
			t.Errorf("expected New Profile button present")
		}
		if !strings.Contains(body, prof.Name) {
			t.Errorf("expected profile name in list")
		}
		if !strings.Contains(body, "/profiles/") {
			t.Errorf("expected link to profile detail")
		}
		// CRUD: Delete button present on list row
		if !strings.Contains(body, "Delete</button>") {
			t.Errorf("expected Delete button in list")
		}
	})

	t.Run("Detail Page", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/profiles/"+prof.ID, nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()
		ts.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("/profiles/{id} status got %d", w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, "Profile:") || !strings.Contains(body, prof.Name) {
			t.Errorf("expected profile title")
		}
		if !strings.Contains(body, "Versions") {
			t.Errorf("expected versions section")
		}
		// CRUD: edit form controls
		if !strings.Contains(body, "Save") || !strings.Contains(body, "Delete Profile") || !strings.Contains(body, "Create Version") {
			t.Errorf("expected edit/delete/create controls on detail page")
		}
		if !strings.Contains(body, "/profiles/"+prof.ID+"/versions/1") || !strings.Contains(body, "/profiles/"+prof.ID+"/versions/2") {
			t.Errorf("expected version links")
		}
	})

	t.Run("Version Page", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/profiles/"+prof.ID+"/versions/2", nil)
		ts.addAuth(req)
		w := httptest.NewRecorder()
		ts.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("/profiles/{id}/versions/{v} status got %d", w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, "Entries") {
			t.Errorf("expected entries section")
		}
		if !strings.Contains(body, "AssetTag") || !strings.Contains(body, "ASSET-002") {
			t.Errorf("expected entry details visible")
		}
		// CRUD: delete version button
		if !strings.Contains(body, "Delete Version") {
			t.Errorf("expected Delete Version button present")
		}
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

func TestProfilesApplyExecute(t *testing.T) {
	ts := createTestSetup(t)
	defer func() { _ = ts.DB.Close() }()

	// Start a local HTTP server to act as the BMC
	patchCalls := make([]string, 0, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			b, _ := io.ReadAll(r.Body)
			patchCalls = append(patchCalls, r.URL.Path+"|"+string(b))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":"ok"}`))
			return
		}
		if r.Method == http.MethodGet {
			// Minimal Redfish discovery responses
			switch r.URL.Path {
			case "/redfish/v1/Systems":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/sys1"}]}`))
			case "/redfish/v1/Systems/sys1/Bios":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"@Redfish.Settings":{"SettingsObject":{"@odata.id":"/redfish/v1/Systems/sys1/Bios/Settings"}},"Attributes":{"LogicalProc":true,"BootMode":"UEFI"}}`))
			case "/redfish/v1/Managers":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Managers/mgr1"}]}`))
			case "/redfish/v1/Managers/mgr1/NetworkProtocol":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"@Redfish.Settings":{"SettingsObject":{"@odata.id":"/redfish/v1/Managers/mgr1/NetworkProtocol/Settings"}},"HTTPS":{"Port":443},"NTP":{"ProtocolEnabled":false}}`))
			default:
				http.NotFound(w, r)
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ctx := context.Background()
	// Create BMC pointing to test server (use explicit http scheme)
	if err := ts.DB.CreateBMC(ctx, &models.BMC{Name: "b1", Address: srv.URL, Username: "u", Password: "p", Enabled: true}); err != nil {
		t.Fatalf("create bmc: %v", err)
	}

	// Create a profile with two entries: BIOS and NetworkProtocol
	prof := &models.Profile{Name: "p1"}
	if err := ts.DB.CreateProfile(ctx, prof); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	v := &models.ProfileVersion{ProfileID: prof.ID, Version: 1, Entries: []models.ProfileEntry{
		{ResourcePath: "/redfish/v1/Systems/sys1/Bios", Attribute: "Attributes.LogicalProc", DesiredValue: false},
		{ResourcePath: "/redfish/v1/Managers/mgr1/NetworkProtocol", Attribute: "HTTPS.Port", DesiredValue: 444},
	}}
	if err := ts.DB.CreateProfileVersion(ctx, v); err != nil {
		t.Fatalf("create version: %v", err)
	}

	// Perform apply (non-dry-run)
	body := map[string]any{"bmc": "b1", "dryRun": false, "continueOnError": false, "version": 1}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/"+prof.ID+"/apply", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	ts.addAuth(req)
	rr := httptest.NewRecorder()
	ts.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		DryRun  bool `json:"dry_run"`
		Summary struct {
			RequestCount int `json:"request_count"`
			Success      int `json:"success"`
			Failed       int `json:"failed"`
		} `json:"summary"`
		Results []struct {
			TargetPath string
			OK         bool
		} `json:"results"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp.DryRun {
		t.Fatalf("expected execution, got dry_run")
	}
	if resp.Summary.RequestCount == 0 || len(resp.Results) == 0 {
		t.Fatalf("expected results; got %+v", resp)
	}
	for _, r := range resp.Results {
		if !r.OK {
			t.Fatalf("request failed: %+v", r)
		}
	}
	if len(patchCalls) == 0 {
		t.Fatalf("expected patch calls to test server")
	}

	// Ensure an audit record exists
	recs, err := ts.DB.ListAudits(ctx, "", 10)
	if err != nil {
		t.Fatalf("list audits: %v", err)
	}
	if len(recs) == 0 {
		t.Fatalf("expected audits created")
	}
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
		if !strings.Contains(body, "Changes (Audit)") {
			t.Error("Response should contain Changes tab section")
		}
		if !strings.Contains(body, "changes-table") {
			t.Error("Response should contain changes table placeholder")
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

func TestBMCDetails_SnapshotUI(t *testing.T) {
	ts := createTestSetup(t)
	defer func() { _ = ts.DB.Close() }()

	ctx := context.Background()

	// Seed a BMC so details page renders for that name
	b := &models.BMC{Name: "snap-bmc", Address: "10.0.0.5", Username: "u", Password: "p", Enabled: true}
	if err := ts.DB.CreateBMC(ctx, b); err != nil {
		t.Fatalf("create bmc: %v", err)
	}

	req := httptest.NewRequest("GET", "/bmcs/details?name=snap-bmc", nil)
	ts.addAuth(req)
	w := httptest.NewRecorder()
	ts.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	// Check snapshot button and modal elements
	if !strings.Contains(body, "Snapshot Current Settings") {
		t.Errorf("expected Snapshot button text present")
	}
	if !strings.Contains(body, "id=\"snap-form\"") || !strings.Contains(body, "id=\"snap-modal\"") {
		t.Errorf("expected snapshot modal elements present")
	}
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

func TestProfilesAPI(t *testing.T) {
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

	// Delete version
	req = httptest.NewRequest(http.MethodDelete, "/api/profiles/"+created.ID+"/versions/1", nil)
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete version expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	// Confirm gone
	req = httptest.NewRequest(http.MethodGet, "/api/profiles/"+created.ID+"/versions/1", nil)
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after deletion, got %d", rec.Code)
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
	defer func() { _ = db.Close() }()

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

func TestBootOrderPreviewAndApply(t *testing.T) {
	ts := createTestSetup(t)
	defer func() { _ = ts.DB.Close() }()

	// Mock BMC exposing ComputerSystem with Boot.BootOrder and allowable values
	var lastPatchPath string
	var lastPatchBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/redfish/v1/Systems":
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/S1"}]}`))
			case "/redfish/v1/Systems/S1":
				// Current BootOrder and Allowable values
				_, _ = w.Write([]byte(`{"Boot":{"BootOrder":["Pxe","Hdd","Usb"]},"BootOrder@Redfish.AllowableValues":["Pxe","Hdd","Usb","Cd"]}`))
			case "/redfish/v1/Managers":
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Managers/M1"}]}`))
			case "/redfish/v1/Managers/M1/NetworkProtocol":
				// minimal to satisfy discovery code paths
				_, _ = w.Write([]byte(`{"@Redfish.Settings":{"SettingsObject":{"@odata.id":"/redfish/v1/Managers/M1/NetworkProtocol/Settings"}},"HTTPS":{"Port":443}}`))
			default:
				http.NotFound(w, r)
			}
		case http.MethodPatch:
			// Capture BootOrder patch
			b, _ := io.ReadAll(r.Body)
			lastPatchPath = r.URL.Path
			lastPatchBody = string(b)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx := context.Background()
	// Seed BMC with credentials matching server
	if err := ts.DB.CreateBMC(ctx, &models.BMC{Name: "b1", Address: server.URL, Username: "admin", Password: "password", Enabled: true}); err != nil {
		t.Fatalf("create bmc: %v", err)
	}

	h := New(ts.DB)

	// Discover settings to persist BootOrder descriptor
	req := httptest.NewRequest(http.MethodGet, "/api/bmcs/b1/settings", nil)
	ts.addAuth(req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Create a profile with desired BootOrder different from current
	p := models.Profile{Name: "boot-prof"}
	body, _ := json.Marshal(p)
	req = httptest.NewRequest(http.MethodPost, "/api/profiles", bytes.NewReader(body))
	ts.addAuth(req)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create profile: %d %s", rec.Code, rec.Body.String())
	}
	var created models.Profile
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Build a version setting Boot.BootOrder to [Usb,Hdd,Pxe]
	v := models.ProfileVersion{Notes: "set order", Entries: []models.ProfileEntry{{
		ResourcePath: "/redfish/v1/Systems/S1",
		Attribute:    "Boot.BootOrder",
		DesiredValue: []string{"Usb", "Hdd", "Pxe"},
		// Apply time preference aligns with descriptor default (OnReset)
		ApplyTimePreference: "OnReset",
	}}}
	body, _ = json.Marshal(v)
	req = httptest.NewRequest(http.MethodPost, "/api/profiles/"+created.ID+"/versions", bytes.NewReader(body))
	ts.addAuth(req)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create version: %d %s", rec.Code, rec.Body.String())
	}

	// Preview should show a change for Boot.BootOrder
	req = httptest.NewRequest(http.MethodGet, "/api/profiles/"+created.ID+"/preview?bmc=b1", nil)
	ts.addAuth(req)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var pv struct {
		Changes []map[string]any
		Summary struct{ Changes int }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pv); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if pv.Summary.Changes == 0 {
		t.Fatalf("expected at least one change for BootOrder")
	}

	// Apply (execute, not dry-run). Should PATCH Systems/S1 with nested body {"Boot":{"BootOrder":[...]}}
	applyReq := map[string]any{"bmc": "b1", "dryRun": false}
	body, _ = json.Marshal(applyReq)
	req = httptest.NewRequest(http.MethodPost, "/api/profiles/"+created.ID+"/apply", bytes.NewReader(body))
	ts.addAuth(req)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if lastPatchPath != "/redfish/v1/Systems/S1" {
		t.Fatalf("unexpected patch path: %s", lastPatchPath)
	}
	if !strings.Contains(lastPatchBody, "\"BootOrder\"") || !strings.Contains(lastPatchBody, "\"Usb\"") {
		t.Fatalf("patch body missing BootOrder array: %s", lastPatchBody)
	}
}

func TestProfilesApplyDryRunAPI(t *testing.T) {
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

	// Admin user (operator allowed)
	passwordHash, _ := pkgAuth.HashPassword("admin")
	admin := &models.User{ID: "u1", Username: "admin", PasswordHash: passwordHash, Role: models.RoleAdmin, Enabled: true}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Mock BMC exposing redfish like in preview test
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
		t.Fatalf("create bmc: %v", err)
	}

	handler := New(db)

	// Discover to persist snapshot
	req := httptest.NewRequest(http.MethodGet, "/api/bmcs/b1/settings", nil)
	req.SetBasicAuth("admin", "admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings list expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Create profile and version with two entries (one change, one same)
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

	v := models.ProfileVersion{Notes: "v1",
		Entries: []models.ProfileEntry{
			{ResourcePath: "/redfish/v1/Managers/M1/NetworkProtocol", Attribute: "HTTPS.Port", DesiredValue: 444},
			{ResourcePath: "/redfish/v1/Systems/S1/Bios", Attribute: "Attributes.LogicalProc", DesiredValue: true},
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

	// Apply dry-run
	payload := map[string]any{"bmc": "b1", "dryRun": true}
	body, _ = json.Marshal(payload)
	req = httptest.NewRequest(http.MethodPost, "/api/profiles/"+created.ID+"/apply", bytes.NewReader(body))
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply dry-run expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var apply map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &apply); err != nil {
		t.Fatalf("decode apply: %v", err)
	}
	summary, _ := apply["summary"].(map[string]any)
	if summary == nil {
		t.Fatalf("missing summary in response: %s", rec.Body.String())
	}
	if int(summary["total_entries"].(float64)) != 2 {
		t.Fatalf("expected total_entries 2, got %v", summary["total_entries"])
	}
	if int(summary["request_count"].(float64)) != 1 {
		t.Fatalf("expected 1 request, got %v", summary["request_count"])
	}
	reqs, _ := apply["requests"].([]interface{})
	if len(reqs) == 0 {
		t.Fatalf("expected at least one request")
	}
	// Verify one request targets NetworkProtocol and contains HTTPS.Port 444
	found := false
	for _, it := range reqs {
		r := it.(map[string]any)
		rp, _ := r["resource_path"].(string)
		ru, _ := r["request_url"].(string)
		if strings.Contains(rp, "NetworkProtocol") || strings.Contains(ru, "NetworkProtocol") {
			if m := r["http_method"].(string); m != http.MethodPatch {
				t.Fatalf("expected PATCH, got %s", m)
			}
			if body, ok := r["request_body"].(map[string]any); ok {
				if https, ok := body["HTTPS"].(map[string]any); ok {
					if port, ok := https["Port"].(float64); ok {
						if int(port) == 444 {
							found = true
						}
					}
				}
			}
		}
	}
	if !found {
		t.Fatalf("did not find merged HTTPS.Port change in requests: %+v", apply["requests"])
	}
}

func TestProfilesImportExportAPI(t *testing.T) {
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
	defer func() { _ = db.Close() }()

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
	defer func() { _ = db.Close() }()

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

func TestAuditEndpoints(t *testing.T) {
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

	// Create users: admin and operator
	passAdmin, _ := pkgAuth.HashPassword("admin")
	admin := &models.User{ID: "u1", Username: "admin", PasswordHash: passAdmin, Role: models.RoleAdmin, Enabled: true}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	passOp, _ := pkgAuth.HashPassword("op")
	operator := &models.User{ID: "u2", Username: "op", PasswordHash: passOp, Role: models.RoleOperator, Enabled: true}
	if err := db.CreateUser(ctx, operator); err != nil {
		t.Fatalf("create operator: %v", err)
	}

	h := New(db)

	// Seed an audit record directly
	a := &models.AuditRecord{UserID: admin.ID, UserName: admin.Username, BMCName: "b1", Action: "proxy", Method: http.MethodGet, Path: "/redfish/v1/Systems", StatusCode: 200, DurationMS: 5, RequestBody: "{}", ResponseBody: "{}"}
	if err := db.CreateAudit(ctx, a); err != nil {
		t.Fatalf("create audit: %v", err)
	}

	// Admin can list audits
	req := httptest.NewRequest(http.MethodGet, "/api/audit?bmc=b1&limit=10", nil)
	req.SetBasicAuth("admin", "admin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list audits expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected json, got %s", ct)
	}
	if !strings.Contains(rec.Body.String(), a.ID) {
		t.Fatalf("expected response to contain audit id")
	}

	// Filter by method and path substring
	req = httptest.NewRequest(http.MethodGet, "/api/audit?method=GET&path=Systems", nil)
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered list expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var list []models.AuditRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) == 0 || list[0].Method != http.MethodGet || !strings.Contains(list[0].Path, "Systems") {
		t.Fatalf("unexpected filter results: %+v", list)
	}

	// Admin can get detail
	req = httptest.NewRequest(http.MethodGet, "/api/audit/"+a.ID, nil)
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit detail expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var det models.AuditRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &det); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if det.ID != a.ID || det.Path != a.Path {
		t.Fatalf("unexpected detail: %+v", det)
	}

	// Operator allowed (metadata only) and bodies should be hidden
	req = httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	req.SetBasicAuth("op", "op")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("operator should be allowed metadata-only, got %d: %s", rec.Code, rec.Body.String())
	}
	var opList []models.AuditRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &opList); err != nil {
		t.Fatalf("decode operator list: %v", err)
	}
	if len(opList) == 0 || opList[0].RequestBody != "" || opList[0].ResponseBody != "" {
		t.Fatalf("operator should not see bodies: %+v", opList)
	}

	// Admin UI page loads
	req = httptest.NewRequest(http.MethodGet, "/audit", nil)
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit UI expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "" && !strings.Contains(ct, "text/html") {
		// The template base sets HTML; if empty it's still fine for tests
		t.Fatalf("expected html content type, got %s", ct)
	}
}
