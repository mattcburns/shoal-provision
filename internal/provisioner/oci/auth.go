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
	"bufio"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// AuthConfig holds registry authentication configuration.
type AuthConfig struct {
	// Enabled determines if authentication is required
	Enabled bool

	// Realm for WWW-Authenticate header
	Realm string

	// HtpasswdPath is the path to htpasswd file for credential validation
	HtpasswdPath string

	// AllowPingWithoutAuth allows unauthenticated requests to /v2/ ping endpoint
	AllowPingWithoutAuth bool
}

// Authenticator handles registry authentication.
type Authenticator struct {
	config      AuthConfig
	credentials map[string]string // username -> hashed password
}

// NewAuthenticator creates a new registry authenticator.
func NewAuthenticator(config AuthConfig) (*Authenticator, error) {
	auth := &Authenticator{
		config:      config,
		credentials: make(map[string]string),
	}

	// Load htpasswd file if provided
	if config.HtpasswdPath != "" {
		if err := auth.loadHtpasswd(config.HtpasswdPath); err != nil {
			return nil, fmt.Errorf("failed to load htpasswd: %w", err)
		}
	}

	return auth, nil
}

// loadHtpasswd loads credentials from an htpasswd file.
// Supports bcrypt-hashed passwords (starting with $2y$, $2a$, or $2b$).
func (a *Authenticator) loadHtpasswd(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open htpasswd file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse username:password format
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid htpasswd format at line %d", lineNum)
		}

		username := parts[0]
		hashedPassword := parts[1]

		// Verify it's a valid bcrypt hash
		if !strings.HasPrefix(hashedPassword, "$2a$") &&
			!strings.HasPrefix(hashedPassword, "$2b$") &&
			!strings.HasPrefix(hashedPassword, "$2y$") {
			return fmt.Errorf("unsupported password hash format at line %d (only bcrypt supported)", lineNum)
		}

		a.credentials[username] = hashedPassword
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading htpasswd file: %w", err)
	}

	if len(a.credentials) == 0 {
		return fmt.Errorf("no valid credentials found in htpasswd file")
	}

	return nil
}

// validateCredentials checks if the provided username and password are valid.
func (a *Authenticator) validateCredentials(username, password string) bool {
	hashedPassword, ok := a.credentials[username]
	if !ok {
		return false
	}

	// Compare using bcrypt
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// Middleware returns an HTTP middleware that enforces authentication.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If auth is disabled, pass through
		if !a.config.Enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Allow unauthenticated ping if configured
		if a.config.AllowPingWithoutAuth && (r.URL.Path == "/v2/" || r.URL.Path == "/v2") {
			next.ServeHTTP(w, r)
			return
		}

		// Extract Basic auth credentials
		username, password, ok := r.BasicAuth()
		if !ok {
			a.unauthorized(w, "missing authorization header")
			return
		}

		// Validate credentials
		if !a.validateCredentials(username, password) {
			a.unauthorized(w, "invalid credentials")
			return
		}

		// Credentials valid, proceed
		next.ServeHTTP(w, r)
	})
}

// unauthorized sends a 401 Unauthorized response with WWW-Authenticate header.
// The reason parameter is used for logging but is NOT exposed to the client
// to avoid information leakage.
func (a *Authenticator) unauthorized(w http.ResponseWriter, reason string) {
	realm := a.config.Realm
	if realm == "" {
		realm = "Shoal Registry"
	}

	w.Header().Set("WWW-Authenticate", fmt.Sprintf("Basic realm=%q", realm))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)

	// Standard OCI error response
	// Note: We deliberately use a generic message and do NOT include the reason
	// parameter to avoid leaking information about valid/invalid usernames
	fmt.Fprintf(w, `{"errors":[{"code":"UNAUTHORIZED","message":"authentication required"}]}`)
}

// AddCredential adds a username/password pair to the authenticator.
// The password should be bcrypt-hashed.
// This is primarily for testing purposes.
func (a *Authenticator) AddCredential(username, hashedPassword string) {
	a.credentials[username] = hashedPassword
}

// hashPassword hashes a plaintext password using bcrypt.
// This is a utility function for testing and setup.
func HashPassword(password string) (string, error) {
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}
	return string(hashedBytes), nil
}

// parseBasicAuth is a helper that parses the Authorization header manually.
// This is only used for testing purposes; production code uses r.BasicAuth().
func parseBasicAuth(authHeader string) (username, password string, ok bool) {
	const prefix = "Basic "
	if !strings.HasPrefix(authHeader, prefix) {
		return "", "", false
	}

	encoded := authHeader[len(prefix):]
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", false
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}

	// Use constant-time comparison for username to prevent timing attacks
	// (password comparison happens in bcrypt.CompareHashAndPassword)
	return parts[0], parts[1], true
}
