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

package api

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestIsValidCorrelationID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid UUID", "550e8400-e29b-41d4-a716-446655440000", true},
		{"valid alphanumeric", "abc123xyz789", true},
		{"valid with hyphens", "test-correlation-id-123", true},
		{"valid with underscores", "test_correlation_id_123", true},
		{"empty string", "", false},
		{"single char", "a", true}, // No minimum length beyond > 0
		{"exactly 128 chars", "12345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678", true},
		{"129 chars", "123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789", false},
		{"invalid characters spaces", "invalid correlation id", false},
		{"invalid characters symbols", "test@correlation#id", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidCorrelationID(tt.input)
			if got != tt.want {
				t.Errorf("isValidCorrelationID(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestComputeETag(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "simple string",
			input: []byte("test"),
			want:  `"` + computeSHA256("test") + `"`,
		},
		{
			name:  "JSON data",
			input: []byte(`{"key":"value"}`),
			want:  `"` + computeSHA256(`{"key":"value"}`) + `"`,
		},
		{
			name:  "empty bytes",
			input: []byte{},
			want:  `"` + computeSHA256("") + `"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeETag(tt.input)
			if got != tt.want {
				t.Errorf("computeETag(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSha256Sum(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "simple string",
			input: []byte("test"),
			want:  computeSHA256("test"),
		},
		{
			name:  "empty bytes",
			input: []byte{},
			want:  computeSHA256(""),
		},
		{
			name:  "special characters",
			input: []byte("test@#$%^&*()"),
			want:  computeSHA256("test@#$%^&*()"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sha256Sum(tt.input)
			if got != tt.want {
				t.Errorf("sha256Sum(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// Helper function to compute SHA-256 hash for test validation
func computeSHA256(s string) string {
	hash := sha256.Sum256([]byte(s))
	return hex.EncodeToString(hash[:])
}

func TestSeverityForStatus(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   string
	}{
		{"OK 200", 200, "OK"},
		{"Created 201", 201, "OK"},
		{"No Content 204", 204, "OK"},
		{"Bad Request 400", 400, "Warning"},
		{"Unauthorized 401", 401, "Warning"},
		{"Forbidden 403", 403, "Warning"},
		{"Not Found 404", 404, "Warning"},
		{"Internal Server Error 500", 500, "Critical"},
		{"Service Unavailable 503", 503, "Critical"},
		{"Unknown status 999", 999, "Warning"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := severityForStatus(tt.status)
			if got != tt.want {
				t.Errorf("severityForStatus(%d) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestResolutionForMessageID(t *testing.T) {
	tests := []struct {
		name      string
		messageID string
		want      string
	}{
		{
			name:      "ResourceNotFound",
			messageID: "Base.1.0.ResourceNotFound",
			want:      "Provide a valid resource identifier and resubmit the request.",
		},
		{
			name:      "MethodNotAllowed",
			messageID: "Base.1.0.MethodNotAllowed",
			want:      "Use an allowed HTTP method for the target resource and resubmit the request.",
		},
		{
			name:      "Unauthorized",
			messageID: "Base.1.0.Unauthorized",
			want:      "Provide valid credentials and resubmit the request.",
		},
		{
			name:      "InsufficientPrivilege",
			messageID: "Base.1.0.InsufficientPrivilege",
			want:      "Resubmit the request using an account with the required privileges.",
		},
		{
			name:      "PropertyValueNotInList",
			messageID: "Base.1.0.PropertyValueNotInList",
			want:      "Use a supported value for the property and resubmit the request.",
		},
		{
			name:      "InternalError",
			messageID: "Base.1.0.InternalError",
			want:      "Retry the operation; if the problem persists, contact the service provider.",
		},
		{
			name:      "UnknownMessageID",
			messageID: "Unknown.1.0.SomeMessage",
			want:      "Retry the operation; if the problem persists, contact the service provider.",
		},
		{
			name:      "EmptyMessageID",
			messageID: "",
			want:      "Retry the operation; if the problem persists, contact the service provider.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolutionForMessageID(tt.messageID)
			if got != tt.want {
				t.Errorf("resolutionForMessageID(%q) = %q, want %q", tt.messageID, got, tt.want)
			}
		})
	}
}
