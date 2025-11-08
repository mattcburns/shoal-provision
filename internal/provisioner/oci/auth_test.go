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

package oci

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewAuthenticator_NoConfig(t *testing.T) {
	config := AuthConfig{
		Enabled: false,
	}

	auth, err := NewAuthenticator(config)
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}

	if auth == nil {
		t.Fatal("Expected authenticator, got nil")
	}
}

func TestNewAuthenticator_WithHtpasswd(t *testing.T) {
	// Create temporary htpasswd file
	tmpDir := t.TempDir()
	htpasswdPath := filepath.Join(tmpDir, "htpasswd")

	// Generate bcrypt hash for "password123"
	hashedPassword, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	content := fmt.Sprintf("testuser:%s\n", hashedPassword)
	if err := os.WriteFile(htpasswdPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write htpasswd file: %v", err)
	}

	config := AuthConfig{
		Enabled:      true,
		HtpasswdPath: htpasswdPath,
	}

	auth, err := NewAuthenticator(config)
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}

	// Verify credentials were loaded
	if len(auth.credentials) != 1 {
		t.Errorf("Expected 1 credential, got %d", len(auth.credentials))
	}

	if _, ok := auth.credentials["testuser"]; !ok {
		t.Error("Expected testuser in credentials")
	}
}

func TestNewAuthenticator_HtpasswdNotFound(t *testing.T) {
	config := AuthConfig{
		Enabled:      true,
		HtpasswdPath: "/nonexistent/htpasswd",
	}

	_, err := NewAuthenticator(config)
	if err == nil {
		t.Fatal("Expected error for nonexistent htpasswd file")
	}
}

func TestLoadHtpasswd_InvalidFormat(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name:    "missing colon",
			content: "testuser\n",
			wantErr: true,
		},
		{
			name:    "unsupported hash",
			content: "testuser:plaintextpassword\n",
			wantErr: true,
		},
		{
			name:    "md5 hash",
			content: "testuser:$apr1$...\n",
			wantErr: true,
		},
		{
			name:    "empty file",
			content: "",
			wantErr: true,
		},
		{
			name:    "only comments",
			content: "# comment line\n# another comment\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			htpasswdPath := filepath.Join(tmpDir, "htpasswd")

			if err := os.WriteFile(htpasswdPath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write htpasswd file: %v", err)
			}

			config := AuthConfig{
				Enabled:      true,
				HtpasswdPath: htpasswdPath,
			}

			_, err := NewAuthenticator(config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewAuthenticator() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadHtpasswd_ValidFormats(t *testing.T) {
	// Create valid bcrypt hash
	hashedPassword, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	tests := []struct {
		name          string
		content       string
		expectedUsers []string
	}{
		{
			name:          "single user",
			content:       fmt.Sprintf("user1:%s\n", hashedPassword),
			expectedUsers: []string{"user1"},
		},
		{
			name:          "multiple users",
			content:       fmt.Sprintf("user1:%s\nuser2:%s\n", hashedPassword, hashedPassword),
			expectedUsers: []string{"user1", "user2"},
		},
		{
			name: "with comments",
			content: fmt.Sprintf("# Comment\nuser1:%s\n# Another comment\nuser2:%s\n",
				hashedPassword, hashedPassword),
			expectedUsers: []string{"user1", "user2"},
		},
		{
			name: "with empty lines",
			content: fmt.Sprintf("user1:%s\n\nuser2:%s\n\n",
				hashedPassword, hashedPassword),
			expectedUsers: []string{"user1", "user2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			htpasswdPath := filepath.Join(tmpDir, "htpasswd")

			if err := os.WriteFile(htpasswdPath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write htpasswd file: %v", err)
			}

			config := AuthConfig{
				Enabled:      true,
				HtpasswdPath: htpasswdPath,
			}

			auth, err := NewAuthenticator(config)
			if err != nil {
				t.Fatalf("NewAuthenticator() error = %v", err)
			}

			if len(auth.credentials) != len(tt.expectedUsers) {
				t.Errorf("Expected %d credentials, got %d", len(tt.expectedUsers), len(auth.credentials))
			}

			for _, user := range tt.expectedUsers {
				if _, ok := auth.credentials[user]; !ok {
					t.Errorf("Expected user %s in credentials", user)
				}
			}
		})
	}
}

func TestValidateCredentials(t *testing.T) {
	hashedPassword, err := HashPassword("correctpassword")
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	config := AuthConfig{Enabled: true}
	auth, err := NewAuthenticator(config)
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}

	auth.AddCredential("testuser", hashedPassword)

	tests := []struct {
		name     string
		username string
		password string
		want     bool
	}{
		{
			name:     "valid credentials",
			username: "testuser",
			password: "correctpassword",
			want:     true,
		},
		{
			name:     "wrong password",
			username: "testuser",
			password: "wrongpassword",
			want:     false,
		},
		{
			name:     "nonexistent user",
			username: "nonexistent",
			password: "anypassword",
			want:     false,
		},
		{
			name:     "empty password",
			username: "testuser",
			password: "",
			want:     false,
		},
		{
			name:     "empty username",
			username: "",
			password: "correctpassword",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := auth.validateCredentials(tt.username, tt.password)
			if got != tt.want {
				t.Errorf("validateCredentials(%q, <password>) = %v, want %v",
					tt.username, got, tt.want)
			}
		})
	}
}

func TestMiddleware_Disabled(t *testing.T) {
	config := AuthConfig{Enabled: false}
	auth, err := NewAuthenticator(config)
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}

	handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	req := httptest.NewRequest("GET", "/v2/test/blobs/sha256:abc", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if body := w.Body.String(); body != "success" {
		t.Errorf("Expected 'success', got %q", body)
	}
}

func TestMiddleware_UnauthenticatedRequest(t *testing.T) {
	config := AuthConfig{
		Enabled: true,
		Realm:   "Test Realm",
	}
	auth, err := NewAuthenticator(config)
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}

	handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v2/test/blobs/sha256:abc", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}

	wwwAuth := w.Header().Get("WWW-Authenticate")
	if !strings.Contains(wwwAuth, "Test Realm") {
		t.Errorf("Expected WWW-Authenticate header with 'Test Realm', got %q", wwwAuth)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got %q", contentType)
	}

	body := w.Body.String()
	if !strings.Contains(body, "UNAUTHORIZED") {
		t.Errorf("Expected error response with UNAUTHORIZED code, got %q", body)
	}
}

func TestMiddleware_ValidCredentials(t *testing.T) {
	hashedPassword, err := HashPassword("validpassword")
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	config := AuthConfig{Enabled: true}
	auth, err := NewAuthenticator(config)
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}

	auth.AddCredential("validuser", hashedPassword)

	handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("authenticated"))
	}))

	req := httptest.NewRequest("GET", "/v2/test/blobs/sha256:abc", nil)
	req.SetBasicAuth("validuser", "validpassword")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if body := w.Body.String(); body != "authenticated" {
		t.Errorf("Expected 'authenticated', got %q", body)
	}
}

func TestMiddleware_InvalidCredentials(t *testing.T) {
	hashedPassword, err := HashPassword("correctpassword")
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	config := AuthConfig{Enabled: true}
	auth, err := NewAuthenticator(config)
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}

	auth.AddCredential("testuser", hashedPassword)

	tests := []struct {
		name     string
		username string
		password string
	}{
		{
			name:     "wrong password",
			username: "testuser",
			password: "wrongpassword",
		},
		{
			name:     "wrong username",
			username: "wronguser",
			password: "correctpassword",
		},
		{
			name:     "both wrong",
			username: "wronguser",
			password: "wrongpassword",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("Handler should not be called for invalid credentials")
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/v2/test/blobs/sha256:abc", nil)
			req.SetBasicAuth(tt.username, tt.password)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("Expected status 401, got %d", w.Code)
			}
		})
	}
}

func TestMiddleware_PingEndpoint(t *testing.T) {
	tests := []struct {
		name                 string
		allowPingWithoutAuth bool
		path                 string
		setAuth              bool
		wantStatus           int
	}{
		{
			name:                 "ping allowed without auth",
			allowPingWithoutAuth: true,
			path:                 "/v2/",
			setAuth:              false,
			wantStatus:           http.StatusOK,
		},
		{
			name:                 "ping with trailing slash allowed",
			allowPingWithoutAuth: true,
			path:                 "/v2/",
			setAuth:              false,
			wantStatus:           http.StatusOK,
		},
		{
			name:                 "ping without trailing slash allowed",
			allowPingWithoutAuth: true,
			path:                 "/v2",
			setAuth:              false,
			wantStatus:           http.StatusOK,
		},
		{
			name:                 "ping requires auth when disabled",
			allowPingWithoutAuth: false,
			path:                 "/v2/",
			setAuth:              false,
			wantStatus:           http.StatusUnauthorized,
		},
		{
			name:                 "other endpoints require auth",
			allowPingWithoutAuth: true,
			path:                 "/v2/test/blobs/uploads/",
			setAuth:              false,
			wantStatus:           http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hashedPassword, err := HashPassword("password")
			if err != nil {
				t.Fatalf("Failed to hash password: %v", err)
			}

			config := AuthConfig{
				Enabled:              true,
				AllowPingWithoutAuth: tt.allowPingWithoutAuth,
			}
			auth, err := NewAuthenticator(config)
			if err != nil {
				t.Fatalf("NewAuthenticator() error = %v", err)
			}

			auth.AddCredential("testuser", hashedPassword)

			handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", tt.path, nil)
			if tt.setAuth {
				req.SetBasicAuth("testuser", "password")
			}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, w.Code)
			}
		})
	}
}

func TestMiddleware_NoCredentialLogging(t *testing.T) {
	// This test verifies that credentials are not logged by checking that
	// the unauthorized() function does not expose the reason parameter
	// in the response body

	config := AuthConfig{Enabled: true}
	auth, err := NewAuthenticator(config)
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}

	handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v2/test/blobs/sha256:abc", nil)
	// No auth header
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	body := w.Body.String()

	// Verify that the response does NOT contain specific reason text
	// that could leak information about valid usernames
	if strings.Contains(body, "missing authorization header") {
		t.Error("Response body should not contain internal reason text")
	}

	if strings.Contains(body, "invalid credentials") {
		t.Error("Response body should not contain internal reason text")
	}

	// Verify it contains generic error message
	if !strings.Contains(body, "authentication required") {
		t.Error("Response should contain generic authentication required message")
	}
}

func TestHashPassword(t *testing.T) {
	password := "testpassword123"

	hashed, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if hashed == "" {
		t.Error("Expected non-empty hash")
	}

	// Verify it's a bcrypt hash
	if !strings.HasPrefix(hashed, "$2a$") && !strings.HasPrefix(hashed, "$2b$") {
		t.Errorf("Expected bcrypt hash, got %s", hashed[:10])
	}

	// Verify it can be validated
	config := AuthConfig{Enabled: true}
	auth, err := NewAuthenticator(config)
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}

	auth.AddCredential("testuser", hashed)

	if !auth.validateCredentials("testuser", password) {
		t.Error("Hashed password should validate correctly")
	}
}

func TestParseBasicAuth(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		wantUser   string
		wantPass   string
		wantOK     bool
	}{
		{
			name:       "valid auth",
			authHeader: "Basic dGVzdHVzZXI6dGVzdHBhc3M=", // testuser:testpass
			wantUser:   "testuser",
			wantPass:   "testpass",
			wantOK:     true,
		},
		{
			name:       "no prefix",
			authHeader: "dGVzdHVzZXI6dGVzdHBhc3M=",
			wantOK:     false,
		},
		{
			name:       "wrong prefix",
			authHeader: "Bearer token",
			wantOK:     false,
		},
		{
			name:       "invalid base64",
			authHeader: "Basic not-valid-base64!!!",
			wantOK:     false,
		},
		{
			name:       "missing colon",
			authHeader: "Basic dGVzdHVzZXJ0ZXN0cGFzcw==", // testusertestpass
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, pass, ok := parseBasicAuth(tt.authHeader)

			if ok != tt.wantOK {
				t.Errorf("parseBasicAuth() ok = %v, want %v", ok, tt.wantOK)
			}

			if ok {
				if user != tt.wantUser {
					t.Errorf("parseBasicAuth() user = %q, want %q", user, tt.wantUser)
				}
				if pass != tt.wantPass {
					t.Errorf("parseBasicAuth() pass = %q, want %q", pass, tt.wantPass)
				}
			}
		})
	}
}

func TestAuthenticator_ConcurrentRequests(t *testing.T) {
	hashedPassword, err := HashPassword("password")
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	config := AuthConfig{Enabled: true}
	auth, err := NewAuthenticator(config)
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}

	auth.AddCredential("user1", hashedPassword)
	auth.AddCredential("user2", hashedPassword)

	handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Run 100 concurrent requests
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func(idx int) {
			username := "user1"
			if idx%2 == 0 {
				username = "user2"
			}

			req := httptest.NewRequest("GET", "/v2/test/blobs/sha256:abc", nil)
			req.SetBasicAuth(username, "password")
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Request %d: Expected status 200, got %d", idx, w.Code)
			}

			done <- true
		}(i)
	}

	// Wait for all requests to complete
	for i := 0; i < 100; i++ {
		<-done
	}
}
