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

package crypto

import (
	"regexp"
	"strings"
)

// RedactSecret redacts a secret string for logging.
// Empty strings return empty. Short strings (<=4 chars) return "****".
// Longer strings show first 2 and last 2 characters with asterisks in between.
func RedactSecret(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 4 {
		return "****"
	}
	return secret[:2] + strings.Repeat("*", len(secret)-4) + secret[len(secret)-2:]
}

// RedactToken redacts a bearer token or API token for logging.
// Shows first 4 and last 4 characters with ellipsis.
func RedactToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return "********"
	}
	return token[:4] + "…" + token[len(token)-4:]
}

// RedactPassword always returns "[REDACTED]" for any non-empty password.
// This ensures no password information leaks in logs.
func RedactPassword(password string) string {
	if password == "" {
		return ""
	}
	return "[REDACTED]"
}

// RedactAuthHeader redacts an Authorization header value.
// For Basic auth, redacts the base64-encoded credentials.
// For Bearer tokens, redacts the token.
func RedactAuthHeader(authHeader string) string {
	if authHeader == "" {
		return ""
	}

	// Check for "Basic " prefix
	if strings.HasPrefix(authHeader, "Basic ") {
		return "Basic [REDACTED]"
	}

	// Check for "Bearer " prefix
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		return "Bearer " + RedactToken(token)
	}

	// Unknown scheme, redact entirely
	return "[REDACTED]"
}

// RedactURL redacts sensitive information in URLs (passwords in connection strings).
// Example: postgresql://user:password@host/db -> postgresql://user:****@host/db
func RedactURL(urlStr string) string {
	if urlStr == "" {
		return ""
	}

	// Pattern: scheme://user:password@host
	re := regexp.MustCompile(`(://[^:]+):([^@]+)@`)
	return re.ReplaceAllString(urlStr, "$1:****@")
}

// SensitiveHeaders is a list of HTTP headers that contain sensitive data
// and should never be logged.
var SensitiveHeaders = []string{
	"Authorization",
	"X-Auth-Token",
	"X-Webhook-Secret",
	"Cookie",
	"Set-Cookie",
	"Proxy-Authorization",
	"WWW-Authenticate",
	"Authentication-Info",
}

// IsSensitiveHeader checks if a header name is considered sensitive.
func IsSensitiveHeader(headerName string) bool {
	lower := strings.ToLower(headerName)
	for _, sensitive := range SensitiveHeaders {
		if strings.ToLower(sensitive) == lower {
			return true
		}
	}
	return false
}

// RedactHeaders returns a copy of headers map with sensitive values redacted.
func RedactHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}

	redacted := make(map[string]string, len(headers))
	for k, v := range headers {
		if IsSensitiveHeader(k) {
			if strings.EqualFold(k, "Authorization") {
				redacted[k] = RedactAuthHeader(v)
			} else {
				redacted[k] = "[REDACTED]"
			}
		} else {
			redacted[k] = v
		}
	}
	return redacted
}

// SensitiveJSONFields is a list of JSON field names that typically contain
// sensitive data and should be redacted in logs.
var SensitiveJSONFields = []string{
	"password",
	"secret",
	"token",
	"api_key",
	"apikey",
	"private_key",
	"privatekey",
	"access_key",
	"accesskey",
	"client_secret",
	"webhook_secret",
	"signing_secret",
	"encryption_key",
}

// IsSensitiveField checks if a field name is considered sensitive.
// Case-insensitive comparison.
func IsSensitiveField(fieldName string) bool {
	lower := strings.ToLower(fieldName)
	for _, sensitive := range SensitiveJSONFields {
		if strings.Contains(lower, sensitive) {
			return true
		}
	}
	return false
}

// RedactMap redacts sensitive fields in a map (typically from JSON).
// Returns a new map with sensitive values replaced with "[REDACTED]".
func RedactMap(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}

	redacted := make(map[string]any, len(data))
	for k, v := range data {
		if IsSensitiveField(k) {
			redacted[k] = "[REDACTED]"
		} else {
			// Recursively redact nested maps
			if nestedMap, ok := v.(map[string]any); ok {
				redacted[k] = RedactMap(nestedMap)
			} else {
				redacted[k] = v
			}
		}
	}
	return redacted
}
