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

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders_Basic(t *testing.T) {
	cfg := DefaultSecurityHeadersConfig()
	handler := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify fundamental security headers
	headers := w.Header()

	if got := headers.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options: expected 'nosniff', got %q", got)
	}

	if got := headers.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options: expected 'DENY', got %q", got)
	}

	if got := headers.Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy: expected 'no-referrer', got %q", got)
	}
}

func TestSecurityHeaders_HSTSDisabledByDefault(t *testing.T) {
	cfg := DefaultSecurityHeadersConfig()
	handler := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// HSTS should not be set when disabled
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security should be empty when HSTS disabled, got %q", got)
	}
}

func TestSecurityHeaders_HSTSEnabled(t *testing.T) {
	cfg := SecurityHeadersConfig{
		EnableHSTS:            true,
		HSTSMaxAge:            31536000,
		HSTSIncludeSubdomains: false,
		EnableCORS:            false,
	}
	handler := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	expected := "max-age=31536000"
	if got := w.Header().Get("Strict-Transport-Security"); got != expected {
		t.Errorf("Strict-Transport-Security: expected %q, got %q", expected, got)
	}
}

func TestSecurityHeaders_HSTSWithSubdomains(t *testing.T) {
	cfg := SecurityHeadersConfig{
		EnableHSTS:            true,
		HSTSMaxAge:            63072000,
		HSTSIncludeSubdomains: true,
		EnableCORS:            false,
	}
	handler := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	expected := "max-age=63072000; includeSubDomains"
	if got := w.Header().Get("Strict-Transport-Security"); got != expected {
		t.Errorf("Strict-Transport-Security: expected %q, got %q", expected, got)
	}
}

func TestSecurityHeaders_CORSDisabledByDefault(t *testing.T) {
	cfg := DefaultSecurityHeadersConfig()
	handler := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// CORS headers should not be set when disabled
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin should be empty when CORS disabled, got %q", got)
	}
}

func TestSecurityHeaders_CORSEnabled(t *testing.T) {
	cfg := SecurityHeadersConfig{
		EnableHSTS:         false,
		EnableCORS:         true,
		CORSAllowedOrigins: []string{"https://example.com"},
		CORSAllowedMethods: []string{"GET", "POST"},
		CORSAllowedHeaders: []string{"Content-Type"},
		CORSMaxAge:         7200,
	}
	handler := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify CORS headers on actual request
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("Access-Control-Allow-Origin: expected 'https://example.com', got %q", got)
	}
}

func TestSecurityHeaders_CORSPreflight(t *testing.T) {
	cfg := SecurityHeadersConfig{
		EnableHSTS:         false,
		EnableCORS:         true,
		CORSAllowedOrigins: []string{"https://app.example.com"},
		CORSAllowedMethods: []string{"GET", "POST", "PUT"},
		CORSAllowedHeaders: []string{"Content-Type", "Authorization"},
		CORSMaxAge:         3600,
	}
	handler := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This handler should not be called for OPTIONS requests
		t.Error("handler should not be called for OPTIONS preflight")
	}))

	req := httptest.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Preflight should return 204 No Content
	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}

	// Verify CORS preflight headers
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("Access-Control-Allow-Origin: expected 'https://app.example.com', got %q", got)
	}

	if got := w.Header().Get("Access-Control-Allow-Methods"); got != "GET,POST,PUT" {
		t.Errorf("Access-Control-Allow-Methods: expected 'GET,POST,PUT', got %q", got)
	}

	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type,Authorization" {
		t.Errorf("Access-Control-Allow-Headers: expected 'Content-Type,Authorization', got %q", got)
	}

	if got := w.Header().Get("Access-Control-Max-Age"); got != "3600" {
		t.Errorf("Access-Control-Max-Age: expected '3600', got %q", got)
	}
}

func TestSecurityHeaders_CORSMultipleOrigins(t *testing.T) {
	cfg := SecurityHeadersConfig{
		EnableHSTS:         false,
		EnableCORS:         true,
		CORSAllowedOrigins: []string{"https://example.com", "https://app.example.com"},
		CORSAllowedMethods: []string{"GET"},
		CORSAllowedHeaders: []string{"Content-Type"},
		CORSMaxAge:         0,
	}
	handler := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	expected := "https://example.com,https://app.example.com"
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != expected {
		t.Errorf("Access-Control-Allow-Origin: expected %q, got %q", expected, got)
	}
}

func TestSecurityHeaders_CORSMaxAgeZero(t *testing.T) {
	cfg := SecurityHeadersConfig{
		EnableHSTS:         false,
		EnableCORS:         true,
		CORSAllowedOrigins: []string{"*"},
		CORSAllowedMethods: []string{"GET"},
		CORSAllowedHeaders: []string{"Content-Type"},
		CORSMaxAge:         0, // Zero should not set header
	}
	handler := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("OPTIONS", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Max-Age should not be set when zero
	if got := w.Header().Get("Access-Control-Max-Age"); got != "" {
		t.Errorf("Access-Control-Max-Age should be empty when zero, got %q", got)
	}
}

func TestDefaultSecurityHeadersConfig(t *testing.T) {
	cfg := DefaultSecurityHeadersConfig()

	if cfg.EnableHSTS {
		t.Error("HSTS should be disabled by default")
	}

	if cfg.HSTSMaxAge != 31536000 {
		t.Errorf("HSTSMaxAge: expected 31536000, got %d", cfg.HSTSMaxAge)
	}

	if cfg.HSTSIncludeSubdomains {
		t.Error("HSTSIncludeSubdomains should be false by default")
	}

	if cfg.EnableCORS {
		t.Error("CORS should be disabled by default")
	}

	if len(cfg.CORSAllowedOrigins) == 0 {
		t.Error("CORSAllowedOrigins should have default values")
	}

	if len(cfg.CORSAllowedMethods) == 0 {
		t.Error("CORSAllowedMethods should have default values")
	}
}
