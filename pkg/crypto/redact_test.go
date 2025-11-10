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
	"strings"
	"testing"
)

func TestRedactSecret(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty", "", ""},
		{"short 1 char", "a", "****"},
		{"short 4 chars", "abcd", "****"},
		{"medium 8 chars", "12345678", "12****78"},
		{"long", "my-secret-key-12345", "my***************45"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactSecret(tt.input)
			if result != tt.expected {
				t.Errorf("RedactSecret(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestRedactToken(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty", "", ""},
		{"short", "abc", "********"},
		{"8 chars", "12345678", "********"},
		{"long", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9", "eyJh…VCJ9"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactToken(tt.input)
			if result != tt.expected {
				t.Errorf("RedactToken(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestRedactPassword(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty", "", ""},
		{"short", "pwd", "[REDACTED]"},
		{"long", "super-secret-password-123", "[REDACTED]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactPassword(tt.input)
			if result != tt.expected {
				t.Errorf("RedactPassword(%q) = %q, want %q", tt.input, result, tt.expected)
			}

			// Ensure no part of original password is visible
			if tt.input != "" && strings.Contains(result, tt.input) {
				t.Errorf("RedactPassword should not contain original password")
			}
		})
	}
}

func TestRedactAuthHeader(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty", "", ""},
		{"basic auth", "Basic dXNlcjpwYXNzd29yZA==", "Basic [REDACTED]"},
		{"bearer token short", "Bearer abc123", "Bearer ********"},
		{"bearer token long", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.payload.signature", "Bearer eyJh…ture"},
		{"unknown scheme", "CustomAuth secret123", "[REDACTED]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactAuthHeader(tt.input)
			if result != tt.expected {
				t.Errorf("RedactAuthHeader(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestRedactURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty", "", ""},
		{"no password", "https://example.com/api", "https://example.com/api"},
		{"postgres with password", "postgresql://user:password123@localhost/db", "postgresql://user:****@localhost/db"},
		{"mysql with password", "mysql://admin:secretpwd@db.example.com:3306/mydb", "mysql://admin:****@db.example.com:3306/mydb"},
		{"http with password", "http://user:pass@api.example.com/endpoint", "http://user:****@api.example.com/endpoint"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactURL(tt.input)
			if result != tt.expected {
				t.Errorf("RedactURL(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsSensitiveHeader(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected bool
	}{
		{"Authorization", "Authorization", true},
		{"authorization lowercase", "authorization", true},
		{"X-Auth-Token", "X-Auth-Token", true},
		{"X-Webhook-Secret", "X-Webhook-Secret", true},
		{"Cookie", "Cookie", true},
		{"Content-Type", "Content-Type", false},
		{"User-Agent", "User-Agent", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSensitiveHeader(tt.header)
			if result != tt.expected {
				t.Errorf("IsSensitiveHeader(%q) = %v, want %v", tt.header, result, tt.expected)
			}
		})
	}
}

func TestRedactHeaders(t *testing.T) {
	input := map[string]string{
		"Content-Type":     "application/json",
		"Authorization":    "Bearer token123",
		"X-Webhook-Secret": "secret456",
		"User-Agent":       "test-agent/1.0",
	}

	result := RedactHeaders(input)

	// Check non-sensitive headers are preserved
	if result["Content-Type"] != "application/json" {
		t.Error("Content-Type should not be redacted")
	}
	if result["User-Agent"] != "test-agent/1.0" {
		t.Error("User-Agent should not be redacted")
	}

	// Check sensitive headers are redacted
	if result["Authorization"] == "Bearer token123" {
		t.Error("Authorization should be redacted")
	}
	if strings.Contains(result["Authorization"], "token123") {
		t.Error("Authorization should not contain original token")
	}

	if result["X-Webhook-Secret"] != "[REDACTED]" {
		t.Errorf("X-Webhook-Secret should be [REDACTED], got %q", result["X-Webhook-Secret"])
	}

	// Original map should not be modified
	if input["Authorization"] != "Bearer token123" {
		t.Error("original map should not be modified")
	}
}

func TestRedactHeaders_Nil(t *testing.T) {
	result := RedactHeaders(nil)
	if result != nil {
		t.Error("RedactHeaders(nil) should return nil")
	}
}

func TestIsSensitiveField(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		expected bool
	}{
		{"password", "password", true},
		{"Password uppercase", "Password", true},
		{"user_password", "user_password", true},
		{"secret", "secret", true},
		{"webhook_secret", "webhook_secret", true},
		{"api_key", "api_key", true},
		{"token", "token", true},
		{"access_token", "access_token", true},
		{"username", "username", false},
		{"email", "email", false},
		{"name", "name", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSensitiveField(tt.field)
			if result != tt.expected {
				t.Errorf("IsSensitiveField(%q) = %v, want %v", tt.field, result, tt.expected)
			}
		})
	}
}

func TestRedactMap(t *testing.T) {
	input := map[string]any{
		"username": "admin",
		"password": "secret123",
		"email":    "admin@example.com",
		"api_key":  "key-12345",
		"config": map[string]any{
			"timeout":        30,
			"webhook_secret": "nested-secret",
		},
	}

	result := RedactMap(input)

	// Check non-sensitive fields preserved
	if result["username"] != "admin" {
		t.Error("username should not be redacted")
	}
	if result["email"] != "admin@example.com" {
		t.Error("email should not be redacted")
	}

	// Check sensitive fields redacted
	if result["password"] != "[REDACTED]" {
		t.Errorf("password should be [REDACTED], got %v", result["password"])
	}
	if result["api_key"] != "[REDACTED]" {
		t.Errorf("api_key should be [REDACTED], got %v", result["api_key"])
	}

	// Check nested map
	config, ok := result["config"].(map[string]any)
	if !ok {
		t.Fatal("config should be a map")
	}
	if config["timeout"] != 30 {
		t.Error("nested timeout should not be redacted")
	}
	if config["webhook_secret"] != "[REDACTED]" {
		t.Errorf("nested webhook_secret should be [REDACTED], got %v", config["webhook_secret"])
	}

	// Original map should not be modified
	if input["password"] != "secret123" {
		t.Error("original map should not be modified")
	}
}

func TestRedactMap_Nil(t *testing.T) {
	result := RedactMap(nil)
	if result != nil {
		t.Error("RedactMap(nil) should return nil")
	}
}

func TestRedactSecret_NoLeakage(t *testing.T) {
	// Ensure redacted output doesn't leak the secret
	secrets := []string{
		"super-secret-key",
		"password123",
		"token-xyz-abc",
	}

	for _, secret := range secrets {
		redacted := RedactSecret(secret)
		// The redacted form should not contain the full original
		if len(secret) > 4 && strings.Contains(redacted, secret) {
			t.Errorf("Redacted form contains original secret: %q -> %q", secret, redacted)
		}
	}
}
