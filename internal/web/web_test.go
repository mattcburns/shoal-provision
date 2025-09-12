package web

import (
	"context"
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
