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

package auth

import (
	"strings"
	"testing"
)

func TestHashPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{
			name:     "Valid password",
			password: "password123",
			wantErr:  false,
		},
		{
			name:     "Complex password",
			password: "P@ssw0rd!#$%^&*()_+-=[]{}|;:,.<>?",
			wantErr:  false,
		},
		{
			name:     "Long password (within bcrypt limit)",
			password: strings.Repeat("a", 72), // bcrypt has a 72-byte limit
			wantErr:  false,
		},
		{
			name:     "Password exceeding bcrypt limit",
			password: strings.Repeat("a", 100),
			wantErr:  true, // bcrypt will error on passwords > 72 bytes
		},
		{
			name:     "Empty password",
			password: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := HashPassword(tt.password)
			if (err != nil) != tt.wantErr {
				t.Errorf("HashPassword() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if hash == "" {
					t.Error("HashPassword() returned empty hash")
				}
				if hash == tt.password {
					t.Error("HashPassword() returned plaintext password")
				}
				if !IsHashed(hash) {
					t.Error("HashPassword() returned invalid hash format")
				}
			}
		})
	}
}

func TestVerifyPassword(t *testing.T) {
	// First, create a known hash
	password := "test-password-123"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("Failed to create test hash: %v", err)
	}

	tests := []struct {
		name     string
		password string
		hash     string
		wantErr  bool
	}{
		{
			name:     "Correct password",
			password: password,
			hash:     hash,
			wantErr:  false,
		},
		{
			name:     "Wrong password",
			password: "wrong-password",
			hash:     hash,
			wantErr:  true,
		},
		{
			name:     "Empty password",
			password: "",
			hash:     hash,
			wantErr:  true,
		},
		{
			name:     "Empty hash",
			password: password,
			hash:     "",
			wantErr:  true,
		},
		{
			name:     "Invalid hash",
			password: password,
			hash:     "not-a-valid-hash",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyPassword(tt.password, tt.hash)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyPassword() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHashUniqueness(t *testing.T) {
	password := "test-password"

	// Hash the same password twice
	hash1, err := HashPassword(password)
	if err != nil {
		t.Fatalf("First hash failed: %v", err)
	}

	hash2, err := HashPassword(password)
	if err != nil {
		t.Fatalf("Second hash failed: %v", err)
	}

	// Hashes should be different due to random salt
	if hash1 == hash2 {
		t.Error("Multiple hashes of the same password should be different")
	}

	// But both should verify correctly
	if err := VerifyPassword(password, hash1); err != nil {
		t.Errorf("First hash verification failed: %v", err)
	}

	if err := VerifyPassword(password, hash2); err != nil {
		t.Errorf("Second hash verification failed: %v", err)
	}
}

func TestIsHashed(t *testing.T) {
	// Create a real hash for testing
	realHash, err := HashPassword("test")
	if err != nil {
		t.Fatalf("Failed to create test hash: %v", err)
	}

	tests := []struct {
		name string
		s    string
		want bool
	}{
		{
			name: "Real bcrypt hash",
			s:    realHash,
			want: true,
		},
		{
			name: "Plaintext password",
			s:    "password123",
			want: false,
		},
		{
			name: "Empty string",
			s:    "",
			want: false,
		},
		{
			name: "Too short",
			s:    "$2a$12$short",
			want: false,
		},
		{
			name: "Wrong prefix",
			s:    "$1a$12$" + strings.Repeat("x", 53),
			want: false,
		},
		{
			name: "Valid $2b$ prefix",
			s:    "$2b$12$" + strings.Repeat("x", 53),
			want: true,
		},
		{
			name: "Valid $2y$ prefix",
			s:    "$2y$12$" + strings.Repeat("x", 53),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsHashed(tt.s); got != tt.want {
				t.Errorf("IsHashed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func BenchmarkHashPassword(b *testing.B) {
	password := "test-password-123"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := HashPassword(password)
		if err != nil {
			b.Fatalf("HashPassword failed: %v", err)
		}
	}
}

func BenchmarkVerifyPassword(b *testing.B) {
	password := "test-password-123"
	hash, err := HashPassword(password)
	if err != nil {
		b.Fatalf("Failed to create hash: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := VerifyPassword(password, hash)
		if err != nil {
			b.Fatalf("VerifyPassword failed: %v", err)
		}
	}
}
