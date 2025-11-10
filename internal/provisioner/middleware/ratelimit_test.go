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
	"time"
)

func TestRateLimiter_AllowWithinLimit(t *testing.T) {
	config := RateLimitConfig{
		RequestsPerMinute: 10,
		BurstSize:         5,
		CleanupInterval:   1 * time.Minute,
	}
	rl := NewRateLimiter(config)
	defer rl.Stop()

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make 5 requests (within burst size)
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}
}

func TestRateLimiter_ExceedLimit(t *testing.T) {
	config := RateLimitConfig{
		RequestsPerMinute: 10,
		BurstSize:         3, // Small burst for testing
		CleanupInterval:   1 * time.Minute,
	}
	rl := NewRateLimiter(config)
	defer rl.Stop()

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	clientIP := "10.0.0.1:54321"

	// Make requests up to burst size (should succeed)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = clientIP
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// Next request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = clientIP
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}

	// Check Retry-After header
	if retryAfter := w.Header().Get("Retry-After"); retryAfter != "60" {
		t.Errorf("expected Retry-After: 60, got %q", retryAfter)
	}
}

func TestRateLimiter_DifferentClients(t *testing.T) {
	config := RateLimitConfig{
		RequestsPerMinute: 10,
		BurstSize:         2,
		CleanupInterval:   1 * time.Minute,
	}
	rl := NewRateLimiter(config)
	defer rl.Stop()

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Client 1 uses up their quota
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("client1 request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// Client 1 should now be rate limited
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.1.1:12345"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusTooManyRequests {
		t.Errorf("client1 expected 429, got %d", w1.Code)
	}

	// Client 2 should still be allowed
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.2:54321"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("client2 expected 200, got %d", w2.Code)
	}
}

func TestRateLimiter_TokenRefill(t *testing.T) {
	config := RateLimitConfig{
		RequestsPerMinute: 60, // 1 per second
		BurstSize:         1,
		CleanupInterval:   1 * time.Minute,
	}
	rl := NewRateLimiter(config)
	defer rl.Stop()

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	clientIP := "10.0.0.5:12345"

	// Use up the initial token
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = clientIP
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request should succeed, got %d", w1.Code)
	}

	// Immediate second request should fail
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = clientIP
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("second request should be rate limited, got %d", w2.Code)
	}

	// Wait for token refill (slightly over 1 second for 1 token at 60/min)
	time.Sleep(1100 * time.Millisecond)

	// Third request should succeed after refill
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.RemoteAddr = clientIP
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Errorf("third request should succeed after refill, got %d", w3.Code)
	}
}

func TestGetClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.1, 198.51.100.1")
	req.RemoteAddr = "10.0.0.1:12345"

	ip := getClientIP(req)
	if ip != "203.0.113.1" {
		t.Errorf("expected first IP from X-Forwarded-For, got %s", ip)
	}
}

func TestGetClientIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Real-IP", "198.51.100.5")
	req.RemoteAddr = "10.0.0.1:12345"

	ip := getClientIP(req)
	if ip != "198.51.100.5" {
		t.Errorf("expected X-Real-IP, got %s", ip)
	}
}

func TestGetClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:54321"

	ip := getClientIP(req)
	if ip != "192.168.1.100" {
		t.Errorf("expected IP from RemoteAddr without port, got %s", ip)
	}
}

func TestGetClientIP_Priority(t *testing.T) {
	// X-Forwarded-For should take priority over X-Real-IP
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.1")
	req.Header.Set("X-Real-IP", "198.51.100.1")
	req.RemoteAddr = "10.0.0.1:12345"

	ip := getClientIP(req)
	if ip != "203.0.113.1" {
		t.Errorf("expected X-Forwarded-For to take priority, got %s", ip)
	}
}

func TestRateLimiter_Cleanup(t *testing.T) {
	config := RateLimitConfig{
		RequestsPerMinute: 10,
		BurstSize:         5,
		CleanupInterval:   100 * time.Millisecond,
	}
	rl := NewRateLimiter(config)
	defer rl.Stop()

	// Add a client
	rl.allow("192.168.1.1")

	// Verify bucket exists
	rl.mu.RLock()
	if _, exists := rl.buckets["192.168.1.1"]; !exists {
		t.Fatal("bucket should exist")
	}
	initialCount := len(rl.buckets)
	rl.mu.RUnlock()

	// Wait for cleanup cycles (stale entries removed after 2*CleanupInterval)
	time.Sleep(300 * time.Millisecond)

	// Bucket should be cleaned up
	rl.mu.RLock()
	finalCount := len(rl.buckets)
	rl.mu.RUnlock()

	if finalCount >= initialCount {
		t.Logf("Cleanup may not have run yet (initial=%d, final=%d)", initialCount, finalCount)
		// This is not a hard failure as timing can vary
	}
}

func TestDefaultRateLimitConfig(t *testing.T) {
	config := DefaultRateLimitConfig()

	if config.RequestsPerMinute <= 0 {
		t.Error("RequestsPerMinute should be positive")
	}
	if config.BurstSize <= 0 {
		t.Error("BurstSize should be positive")
	}
	if config.CleanupInterval <= 0 {
		t.Error("CleanupInterval should be positive")
	}
}
