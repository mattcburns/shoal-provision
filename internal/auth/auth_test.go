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

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shoal/internal/database"
	pkgAuth "shoal/pkg/auth"
	"shoal/pkg/models"
)

func setupTestAuth(t *testing.T) (*Authenticator, *database.DB) {
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

	// Disable foreign key constraints for testing
	if err := db.DisableForeignKeys(); err != nil {
		t.Fatalf("Failed to disable foreign keys: %v", err)
	}

	auth := New(db)
	return auth, db
}

func TestAuthenticateBasic(t *testing.T) {
	auth, db := setupTestAuth(t)
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Create a test user
	passwordHash, _ := pkgAuth.HashPassword("admin")
	testUser := &models.User{
		ID:           "test-admin-id",
		Username:     "admin",
		PasswordHash: passwordHash,
		Role:         "admin",
		Enabled:      true,
	}
	if err := db.CreateUser(ctx, testUser); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Test valid credentials
	user, err := auth.AuthenticateBasic(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("Authentication failed for valid credentials: %v", err)
	}
	if user == nil {
		t.Fatal("User should not be nil for valid credentials")
	}

	if user.Username != "admin" {
		t.Errorf("Expected username 'admin', got %s", user.Username)
	}

	// Test invalid credentials
	user, err = auth.AuthenticateBasic(ctx, "admin", "wrong-password")
	if err == nil {
		t.Error("Authentication should fail for invalid credentials")
	}

	if user != nil {
		t.Error("User should be nil for invalid credentials")
	}

	// Test invalid username
	user, err = auth.AuthenticateBasic(ctx, "invalid-user", "admin")
	if err == nil {
		t.Error("Authentication should fail for invalid username")
	}

	if user != nil {
		t.Error("User should be nil for invalid username")
	}
}

func TestCreateSession(t *testing.T) {
	auth, db := setupTestAuth(t)
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Create a session
	session, err := auth.CreateSession(ctx, "test-user-123")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	if session == nil {
		t.Fatal("Session should not be nil")
	}

	if session.ID == "" {
		t.Error("Session ID should not be empty")
	}

	if session.Token == "" {
		t.Error("Session token should not be empty")
	}

	if session.UserID != "test-user-123" {
		t.Errorf("Expected UserID 'test-user-123', got %s", session.UserID)
	}

	// Verify session expiry is reasonable (should be around 24 hours)
	expectedExpiry := time.Now().Add(23 * time.Hour)
	if session.ExpiresAt.Before(expectedExpiry) {
		t.Error("Session expiry seems too short")
	}

	expectedExpiry = time.Now().Add(25 * time.Hour)
	if session.ExpiresAt.After(expectedExpiry) {
		t.Error("Session expiry seems too long")
	}

	// Verify session was stored in database
	storedSession, err := db.GetSessionByToken(ctx, session.Token)
	if err != nil {
		t.Fatalf("Failed to retrieve session from database: %v", err)
	}

	if storedSession == nil {
		t.Fatal("Session should exist in database")
	}

	if storedSession.ID != session.ID {
		t.Errorf("Expected stored session ID %s, got %s", session.ID, storedSession.ID)
	}
}

func TestAuthenticateToken(t *testing.T) {
	auth, db := setupTestAuth(t)
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Create a test user
	passwordHash, _ := pkgAuth.HashPassword("testpass")
	testUser := &models.User{
		ID:           "test-user-456",
		Username:     "testuser",
		PasswordHash: passwordHash,
		Role:         "admin",
		Enabled:      true,
	}
	if err := db.CreateUser(ctx, testUser); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create a session first
	session, err := auth.CreateSession(ctx, "test-user-456")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Test token authentication
	user, err := auth.AuthenticateToken(ctx, session.Token)
	if err != nil {
		t.Fatalf("Token authentication failed: %v", err)
	}

	if user == nil {
		t.Fatal("User should not be nil for valid token")
	}

	if user.ID != session.UserID {
		t.Errorf("Expected user ID %s, got %s", session.UserID, user.ID)
	}

	// Test invalid token
	user, err = auth.AuthenticateToken(ctx, "invalid-token")
	if err == nil {
		t.Error("Authentication should fail for invalid token")
	}

	if user != nil {
		t.Error("User should be nil for invalid token")
	}
}

func TestDeleteSession(t *testing.T) {
	auth, db := setupTestAuth(t)
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Create a session
	session, err := auth.CreateSession(ctx, "test-user-789")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Verify session exists
	storedSession, err := db.GetSessionByToken(ctx, session.Token)
	if err != nil {
		t.Fatalf("Failed to check session existence: %v", err)
	}
	if storedSession == nil {
		t.Fatal("Session should exist before deletion")
	}

	// Delete session
	err = auth.DeleteSession(ctx, session.Token)
	if err != nil {
		t.Fatalf("Failed to delete session: %v", err)
	}

	// Verify session is gone
	deletedSession, err := db.GetSessionByToken(ctx, session.Token)
	if err != nil {
		t.Fatalf("Failed to check deleted session: %v", err)
	}
	if deletedSession != nil {
		t.Error("Session should not exist after deletion")
	}
}

func TestAuthenticateRequest(t *testing.T) {
	auth, db := setupTestAuth(t)
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Create a test user
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

	// Test basic authentication
	req := httptest.NewRequest("GET", "/test", nil)
	req.SetBasicAuth("admin", "admin")

	user, err := auth.AuthenticateRequest(req)
	if err != nil {
		t.Fatalf("Basic auth request failed: %v", err)
	}
	if user == nil {
		t.Fatal("User should not be nil for valid basic auth")
	}

	// Test token authentication
	session, err := auth.CreateSession(ctx, "test-user")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Auth-Token", session.Token)

	user, err = auth.AuthenticateRequest(req)
	if err != nil {
		t.Fatalf("Token auth request failed: %v", err)
	}
	if user == nil {
		t.Fatal("User should not be nil for valid token")
	}

	// Test no authentication
	req = httptest.NewRequest("GET", "/test", nil)

	user, err = auth.AuthenticateRequest(req)
	if err == nil {
		t.Error("Request should fail with no authentication")
	}
	if user != nil {
		t.Error("User should be nil with no authentication")
	}
}

func TestRequireAuthMiddleware(t *testing.T) {
	auth, db := setupTestAuth(t)
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Create a test user
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

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := GetUserFromContext(r.Context())
		if !ok {
			t.Error("User should be in context")
			http.Error(w, "No user in context", http.StatusInternalServerError)
			return
		}
		if user == nil {
			t.Error("User should not be nil in context")
			http.Error(w, "User is nil", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	})

	// Wrap with auth middleware
	authHandler := auth.RequireAuth(testHandler)

	// Test with valid basic auth
	req := httptest.NewRequest("GET", "/test", nil)
	req.SetBasicAuth("admin", "admin")
	w := httptest.NewRecorder()

	authHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if !strings.Contains(w.Body.String(), "success") {
		t.Error("Expected success response")
	}

	// Test with invalid auth
	req = httptest.NewRequest("GET", "/test", nil)
	req.SetBasicAuth("admin", "wrong-password")
	w = httptest.NewRecorder()

	authHandler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}

	// Verify Redfish-compliant error response
	if !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Error("Expected JSON content type")
	}

	if w.Header().Get("WWW-Authenticate") == "" {
		t.Error("Expected WWW-Authenticate header")
	}

	// Test with no auth
	req = httptest.NewRequest("GET", "/test", nil)
	w = httptest.NewRecorder()

	authHandler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

func TestGetUserFromContext(t *testing.T) {
	// Test with user in context
	user := &models.User{
		ID:       "test-user",
		Username: "testuser",
		Role:     "admin",
		Enabled:  true,
	}

	ctx := context.WithValue(context.Background(), "user", user)

	retrievedUser, ok := GetUserFromContext(ctx)
	if !ok {
		t.Error("Should find user in context")
	}
	if retrievedUser == nil {
		t.Fatal("Retrieved user should not be nil")
	}
	if retrievedUser.ID != user.ID {
		t.Errorf("Expected user ID %s, got %s", user.ID, retrievedUser.ID)
	}

	// Test with no user in context
	ctx = context.Background()

	retrievedUser, ok = GetUserFromContext(ctx)
	if ok {
		t.Error("Should not find user in empty context")
	}
	if retrievedUser != nil {
		t.Error("Retrieved user should be nil")
	}

	// Test with wrong type in context
	ctx = context.WithValue(context.Background(), "user", "not-a-user")

	retrievedUser, ok = GetUserFromContext(ctx)
	if ok {
		t.Error("Should not find user with wrong type")
	}
	if retrievedUser != nil {
		t.Error("Retrieved user should be nil for wrong type")
	}
}

func TestGenerateIDAndToken(t *testing.T) {
	// Test generateID
	id1, err := generateID()
	if err != nil {
		t.Fatalf("Failed to generate ID: %v", err)
	}

	if id1 == "" {
		t.Error("Generated ID should not be empty")
	}

	if len(id1) != 32 { // 16 bytes * 2 hex chars per byte
		t.Errorf("Expected ID length 32, got %d", len(id1))
	}

	// Generate another ID to ensure uniqueness
	id2, err := generateID()
	if err != nil {
		t.Fatalf("Failed to generate second ID: %v", err)
	}

	if id1 == id2 {
		t.Error("Generated IDs should be unique")
	}

	// Test generateToken
	token1, err := generateToken()
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	if token1 == "" {
		t.Error("Generated token should not be empty")
	}

	// Generate another token to ensure uniqueness
	token2, err := generateToken()
	if err != nil {
		t.Fatalf("Failed to generate second token: %v", err)
	}

	if token1 == token2 {
		t.Error("Generated tokens should be unique")
	}
}

func BenchmarkCreateSession(b *testing.B) {
	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "benchmark.db")

	db, err := database.New(dbPath)
	if err != nil {
		b.Fatalf("Failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		b.Fatalf("Migration failed: %v", err)
	}

	auth := New(db)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		session, err := auth.CreateSession(ctx, "bench-user")
		if err != nil {
			b.Fatalf("Failed to create session: %v", err)
		}

		// Clean up for next iteration
		_ = auth.DeleteSession(ctx, session.Token)
	}
}

func BenchmarkAuthenticateBasic(b *testing.B) {
	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "benchmark.db")

	db, err := database.New(dbPath)
	if err != nil {
		b.Fatalf("Failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		b.Fatalf("Migration failed: %v", err)
	}

	auth := New(db)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := auth.AuthenticateBasic(ctx, "admin", "admin")
		if err != nil {
			b.Fatalf("Authentication failed: %v", err)
		}
	}
}
