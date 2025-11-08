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
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewRouter(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	router := NewRouter(storage)
	if router == nil {
		t.Fatal("Expected router, got nil")
	}

	if router.handler == nil {
		t.Error("Expected handler to be initialized")
	}

	if router.authenticator != nil {
		t.Error("Expected authenticator to be nil for NewRouter")
	}
}

func TestNewRouterWithAuth(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	authConfig := AuthConfig{
		Enabled: true,
		Realm:   "Test Registry",
	}

	router, err := NewRouterWithAuth(storage, authConfig)
	if err != nil {
		t.Fatalf("Failed to create router with auth: %v", err)
	}

	if router == nil {
		t.Fatal("Expected router, got nil")
	}

	if router.authenticator == nil {
		t.Error("Expected authenticator to be initialized")
	}
}

func TestSetAuthenticator(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	router := NewRouter(storage)

	if router.authenticator != nil {
		t.Error("Expected authenticator to be nil initially")
	}

	authConfig := AuthConfig{Enabled: true}
	authenticator, err := NewAuthenticator(authConfig)
	if err != nil {
		t.Fatalf("Failed to create authenticator: %v", err)
	}

	router.SetAuthenticator(authenticator)

	if router.authenticator == nil {
		t.Error("Expected authenticator to be set")
	}
}

func TestRouter_ServeHTTP_WithoutAuth(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	router := NewRouter(storage)

	// Test ping endpoint without auth
	req := httptest.NewRequest("GET", "/v2/", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 for ping without auth, got %d", w.Code)
	}
}

func TestRouter_ServeHTTP_WithAuth_Unauthenticated(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	authConfig := AuthConfig{
		Enabled:              true,
		AllowPingWithoutAuth: false,
	}

	router, err := NewRouterWithAuth(storage, authConfig)
	if err != nil {
		t.Fatalf("Failed to create router with auth: %v", err)
	}

	// Test ping endpoint with auth enabled but no credentials
	req := httptest.NewRequest("GET", "/v2/", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 for ping with auth but no credentials, got %d", w.Code)
	}

	wwwAuth := w.Header().Get("WWW-Authenticate")
	if wwwAuth == "" {
		t.Error("Expected WWW-Authenticate header")
	}
}

func TestRouter_ServeHTTP_WithAuth_Authenticated(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	hashedPassword, err := HashPassword("testpass")
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	authConfig := AuthConfig{
		Enabled: true,
	}

	router, err := NewRouterWithAuth(storage, authConfig)
	if err != nil {
		t.Fatalf("Failed to create router with auth: %v", err)
	}

	// Add test credentials
	router.authenticator.AddCredential("testuser", hashedPassword)

	// Test ping endpoint with valid credentials
	req := httptest.NewRequest("GET", "/v2/", nil)
	req.SetBasicAuth("testuser", "testpass")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 for ping with valid credentials, got %d", w.Code)
	}
}

func TestRouter_ServeHTTP_WithAuth_PingAllowedWithoutAuth(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	authConfig := AuthConfig{
		Enabled:              true,
		AllowPingWithoutAuth: true,
	}

	router, err := NewRouterWithAuth(storage, authConfig)
	if err != nil {
		t.Fatalf("Failed to create router with auth: %v", err)
	}

	// Test ping endpoint without credentials when ping is allowed
	req := httptest.NewRequest("GET", "/v2/", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 for ping without auth when allowed, got %d", w.Code)
	}
}

func TestRouter_ServeHTTP_WithAuth_NonPingEndpointRequiresAuth(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	authConfig := AuthConfig{
		Enabled:              true,
		AllowPingWithoutAuth: true,
	}

	router, err := NewRouterWithAuth(storage, authConfig)
	if err != nil {
		t.Fatalf("Failed to create router with auth: %v", err)
	}

	tests := []struct {
		name string
		path string
	}{
		{
			name: "blob upload",
			path: "/v2/test/blobs/uploads/",
		},
		{
			name: "manifest get",
			path: "/v2/test/manifests/latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", tt.path, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("Expected status 401 for %s without auth, got %d", tt.name, w.Code)
			}
		})
	}
}

func TestRouter_ServeHTTP_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	router := NewRouter(storage)

	req := httptest.NewRequest("GET", "/v2/nonexistent/invalid/path", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404 for invalid path, got %d", w.Code)
	}
}
