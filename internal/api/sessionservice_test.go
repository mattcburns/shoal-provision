package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"shoal/internal/database"
	"shoal/pkg/auth"
	"shoal/pkg/models"
)

// setupTestAPI creates a test API handler with a temporary database and admin user
func setupTestAPI(t *testing.T) (http.Handler, *database.DB) {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Create admin user
	passwordHash, err := auth.HashPassword("admin")
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	// Generate user ID
	userIDBytes := make([]byte, 16)
	if _, err := rand.Read(userIDBytes); err != nil {
		t.Fatalf("failed to generate user ID: %v", err)
	}
	userID := hex.EncodeToString(userIDBytes)

	adminUser := &models.User{
		ID:           userID,
		Username:     "admin",
		PasswordHash: passwordHash,
		Role:         models.RoleAdmin,
		Enabled:      true,
	}

	// Create user via DB
	if err := db.CreateUser(ctx, adminUser); err != nil {
		t.Fatalf("failed to create admin user: %v", err)
	}

	apiHandler := New(db)
	return apiHandler, db
}

func TestSessionServiceEndpoints(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer db.Close()

	// 1) Ensure SessionService root requires auth
	req := httptest.NewRequest(http.MethodGet, "/redfish/v1/SessionService", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated SessionService root, got %d", rec.Code)
	}

	// 2) Create a session (login)
	loginBody, _ := json.Marshal(map[string]string{
		"UserName": "admin",
		"Password": "admin",
	})
	req = httptest.NewRequest(http.MethodPost, "/redfish/v1/SessionService/Sessions", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on login, got %d", rec.Code)
	}
	token := rec.Header().Get("X-Auth-Token")
	if token == "" {
		t.Fatalf("expected X-Auth-Token header on login response")
	}
	var loginResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("failed to parse login response: %v", err)
	}
	sessionOdataID, _ := loginResp["@odata.id"].(string)
	if sessionOdataID == "" {
		t.Fatalf("expected @odata.id in login response")
	}
	// Extract session id
	var sessionID string
	{
		parts := bytes.Split([]byte(sessionOdataID), []byte{'/'})
		if len(parts) > 0 {
			sessionID = string(parts[len(parts)-1])
		}
	}
	if sessionID == "" {
		t.Fatalf("failed to extract session id from %q", sessionOdataID)
	}

	// 3) Fetch SessionService root with token
	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/SessionService", nil)
	req.Header.Set("X-Auth-Token", token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for SessionService root with token, got %d", rec.Code)
	}

	// 4) Fetch Sessions collection
	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/SessionService/Sessions", nil)
	req.Header.Set("X-Auth-Token", token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for Sessions collection, got %d", rec.Code)
	}
	var coll map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &coll); err != nil {
		t.Fatalf("failed to parse sessions collection: %v", err)
	}
	if _, ok := coll["Members@odata.count"]; !ok {
		t.Fatalf("expected Members@odata.count in collection")
	}

	// 5) Fetch individual session
	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/SessionService/Sessions/"+sessionID, nil)
	req.Header.Set("X-Auth-Token", token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for session resource, got %d", rec.Code)
	}
	var session map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &session); err != nil {
		t.Fatalf("failed to parse session resource: %v", err)
	}
	if session["UserName"] != "admin" {
		t.Fatalf("expected UserName 'admin', got %v", session["UserName"])
	}

	// 6) Delete the session
	req = httptest.NewRequest(http.MethodDelete, "/redfish/v1/SessionService/Sessions/"+sessionID, nil)
	req.Header.Set("X-Auth-Token", token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on delete session, got %d", rec.Code)
	}

	// 7) Fetch after delete should be 404 (use basic auth since token is now invalid)
	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/SessionService/Sessions/"+sessionID, nil)
	req.SetBasicAuth("admin", "admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for deleted session, got %d", rec.Code)
	}
}
