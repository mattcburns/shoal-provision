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
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimitConfig configures the rate limiter.
type RateLimitConfig struct {
	// RequestsPerMinute is the maximum number of requests allowed per client IP per minute.
	RequestsPerMinute int

	// BurstSize is the maximum burst size (allows short bursts above the rate).
	BurstSize int

	// CleanupInterval is how often to clean up old entries.
	CleanupInterval time.Duration

	// Logger for rate limit events.
	Logger *log.Logger
}

// DefaultRateLimitConfig returns sensible defaults for authentication endpoints.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: 10,
		BurstSize:         5,
		CleanupInterval:   5 * time.Minute,
		Logger:            nil,
	}
}

// clientBucket tracks requests for a single client.
type clientBucket struct {
	tokens     int       // Available tokens
	lastRefill time.Time // Last time tokens were refilled
	mu         sync.Mutex
}

// RateLimiter implements token bucket rate limiting per client IP.
type RateLimiter struct {
	config  RateLimitConfig
	buckets map[string]*clientBucket
	mu      sync.RWMutex
	stop    chan struct{}
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		config:  config,
		buckets: make(map[string]*clientBucket),
		stop:    make(chan struct{}),
	}

	// Start cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// Middleware returns an HTTP middleware that enforces rate limiting.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := getClientIP(r)

		if !rl.allow(clientIP) {
			rl.logf("rate limit exceeded for client=%s path=%s", clientIP, r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "60") // Suggest retry after 1 minute
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "rate_limit_exceeded",
				"message": "Too many requests. Please try again later.",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// allow checks if a request from the given client IP should be allowed.
func (rl *RateLimiter) allow(clientIP string) bool {
	rl.mu.RLock()
	bucket, exists := rl.buckets[clientIP]
	rl.mu.RUnlock()

	if !exists {
		// Create new bucket for this client
		bucket = &clientBucket{
			tokens:     rl.config.BurstSize,
			lastRefill: time.Now(),
		}
		rl.mu.Lock()
		rl.buckets[clientIP] = bucket
		rl.mu.Unlock()
	}

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	// Refill tokens based on time elapsed
	now := time.Now()
	elapsed := now.Sub(bucket.lastRefill)
	tokensToAdd := int(elapsed.Minutes() * float64(rl.config.RequestsPerMinute))

	if tokensToAdd > 0 {
		bucket.tokens += tokensToAdd
		if bucket.tokens > rl.config.BurstSize {
			bucket.tokens = rl.config.BurstSize
		}
		bucket.lastRefill = now
	}

	// Check if we have tokens available
	if bucket.tokens > 0 {
		bucket.tokens--
		return true
	}

	return false
}

// cleanupLoop periodically removes stale client entries.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stop:
			return
		}
	}
}

// cleanup removes client entries that haven't been used recently.
func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	threshold := time.Now().Add(-2 * rl.config.CleanupInterval)
	for ip, bucket := range rl.buckets {
		bucket.mu.Lock()
		if bucket.lastRefill.Before(threshold) {
			delete(rl.buckets, ip)
		}
		bucket.mu.Unlock()
	}
}

// Stop stops the cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.stop)
}

// getClientIP extracts the client IP from the request.
// Checks X-Forwarded-For first, then X-Real-IP, then RemoteAddr.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For (proxy/load balancer)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take first IP in comma-separated list
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr, strip port
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}

func (rl *RateLimiter) logf(format string, args ...any) {
	if rl.config.Logger != nil {
		rl.config.Logger.Printf("[ratelimit] "+format, args...)
	}
}
