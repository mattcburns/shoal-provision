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
	"strconv"
	"strings"
)

// SecurityHeadersConfig holds configuration for security headers middleware.
type SecurityHeadersConfig struct {
	// EnableHSTS enables Strict-Transport-Security header (only for HTTPS)
	EnableHSTS bool
	// HSTSMaxAge is the max-age value for HSTS header (default: 31536000 = 1 year)
	HSTSMaxAge int
	// HSTSIncludeSubdomains adds includeSubDomains to HSTS
	HSTSIncludeSubdomains bool
	// EnableCORS enables CORS headers
	EnableCORS bool
	// CORSAllowedOrigins is the list of allowed origins (default: *)
	CORSAllowedOrigins []string
	// CORSAllowedMethods is the list of allowed HTTP methods
	CORSAllowedMethods []string
	// CORSAllowedHeaders is the list of allowed request headers
	CORSAllowedHeaders []string
	// CORSMaxAge is the max age for CORS preflight cache
	CORSMaxAge int
}

// DefaultSecurityHeadersConfig returns a sensible default configuration
// aligned with OWASP recommendations and design/033_Security_Model.md.
func DefaultSecurityHeadersConfig() SecurityHeadersConfig {
	return SecurityHeadersConfig{
		EnableHSTS:            false, // Only enable when TLS is active
		HSTSMaxAge:            31536000,
		HSTSIncludeSubdomains: false,
		EnableCORS:            false, // Disable by default; opt-in
		CORSAllowedOrigins:    []string{"*"},
		CORSAllowedMethods:    []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		CORSAllowedHeaders:    []string{"Content-Type", "Authorization"},
		CORSMaxAge:            3600,
	}
}

// SecurityHeaders returns middleware that adds security headers to responses.
// This middleware implements OWASP recommendations for secure HTTP headers:
//   - X-Content-Type-Options: nosniff (prevent MIME sniffing)
//   - X-Frame-Options: DENY (prevent clickjacking)
//   - Referrer-Policy: no-referrer (prevent referrer leakage)
//   - Strict-Transport-Security (HSTS) when TLS is enabled
//   - Optional CORS headers when configured
func SecurityHeaders(cfg SecurityHeadersConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Always set fundamental security headers
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "no-referrer")

			// Set HSTS only if enabled (typically only for HTTPS)
			if cfg.EnableHSTS {
				hstsValue := "max-age=" + strconv.Itoa(cfg.HSTSMaxAge)
				if cfg.HSTSIncludeSubdomains {
					hstsValue += "; includeSubDomains"
				}
				w.Header().Set("Strict-Transport-Security", hstsValue)
			}

			// Handle CORS if enabled
			if cfg.EnableCORS {
				// Handle preflight OPTIONS request
				if r.Method == http.MethodOptions {
					w.Header().Set("Access-Control-Allow-Origin", strings.Join(cfg.CORSAllowedOrigins, ","))
					w.Header().Set("Access-Control-Allow-Methods", strings.Join(cfg.CORSAllowedMethods, ","))
					w.Header().Set("Access-Control-Allow-Headers", strings.Join(cfg.CORSAllowedHeaders, ","))
					if cfg.CORSMaxAge > 0 {
						w.Header().Set("Access-Control-Max-Age", strconv.Itoa(cfg.CORSMaxAge))
					}
					w.WriteHeader(http.StatusNoContent)
					return
				}

				// Set CORS headers for actual requests
				w.Header().Set("Access-Control-Allow-Origin", strings.Join(cfg.CORSAllowedOrigins, ","))
			}

			next.ServeHTTP(w, r)
		})
	}
}
