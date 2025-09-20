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

package crypto

import (
	"strings"
	"testing"
)

func TestNewEncryptor(t *testing.T) {
	tests := []struct {
		name       string
		passphrase string
		wantErr    bool
	}{
		{
			name:       "Valid passphrase",
			passphrase: "test-passphrase-123",
			wantErr:    false,
		},
		{
			name:       "Empty passphrase",
			passphrase: "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc, err := NewEncryptor(tt.passphrase)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewEncryptor() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && enc == nil {
				t.Error("NewEncryptor() returned nil encryptor")
			}
		})
	}
}

func TestEncryptDecrypt(t *testing.T) {
	enc, err := NewEncryptor("test-passphrase")
	if err != nil {
		t.Fatalf("Failed to create encryptor: %v", err)
	}

	tests := []struct {
		name      string
		plaintext string
		wantErr   bool
	}{
		{
			name:      "Simple password",
			plaintext: "password123",
			wantErr:   false,
		},
		{
			name:      "Complex password",
			plaintext: "P@ssw0rd!#$%^&*()_+-=[]{}|;:,.<>?",
			wantErr:   false,
		},
		{
			name:      "Long password",
			plaintext: strings.Repeat("a", 1000),
			wantErr:   false,
		},
		{
			name:      "Unicode password",
			plaintext: "密码パスワード🔐",
			wantErr:   false,
		},
		{
			name:      "Empty password",
			plaintext: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encrypt
			encrypted, err := enc.Encrypt(tt.plaintext)
			if (err != nil) != tt.wantErr {
				t.Errorf("Encrypt() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			// Check that encrypted is different from plaintext
			if encrypted == tt.plaintext {
				t.Error("Encrypted text should be different from plaintext")
			}

			// Check that encrypted is base64
			if encrypted == "" {
				t.Error("Encrypted text should not be empty")
			}

			// Decrypt
			decrypted, err := enc.Decrypt(encrypted)
			if err != nil {
				t.Errorf("Decrypt() error = %v", err)
			}

			// Check that decrypted matches original
			if decrypted != tt.plaintext {
				t.Errorf("Decrypted text = %v, want %v", decrypted, tt.plaintext)
			}
		})
	}
}

func TestEncryptionUniqueness(t *testing.T) {
	enc, err := NewEncryptor("test-passphrase")
	if err != nil {
		t.Fatalf("Failed to create encryptor: %v", err)
	}

	plaintext := "password123"

	// Encrypt the same plaintext multiple times
	encrypted1, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("First encryption failed: %v", err)
	}

	encrypted2, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Second encryption failed: %v", err)
	}

	// Due to random nonce, encrypted values should be different
	if encrypted1 == encrypted2 {
		t.Error("Multiple encryptions of the same plaintext should produce different ciphertexts")
	}

	// But both should decrypt to the same plaintext
	decrypted1, err := enc.Decrypt(encrypted1)
	if err != nil {
		t.Fatalf("First decryption failed: %v", err)
	}

	decrypted2, err := enc.Decrypt(encrypted2)
	if err != nil {
		t.Fatalf("Second decryption failed: %v", err)
	}

	if decrypted1 != plaintext || decrypted2 != plaintext {
		t.Error("Both encrypted values should decrypt to the original plaintext")
	}
}

func TestDifferentPassphrases(t *testing.T) {
	enc1, err := NewEncryptor("passphrase1")
	if err != nil {
		t.Fatalf("Failed to create first encryptor: %v", err)
	}

	enc2, err := NewEncryptor("passphrase2")
	if err != nil {
		t.Fatalf("Failed to create second encryptor: %v", err)
	}

	plaintext := "password123"

	// Encrypt with first encryptor
	encrypted, err := enc1.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encryption failed: %v", err)
	}

	// Try to decrypt with second encryptor (should fail)
	_, err = enc2.Decrypt(encrypted)
	if err == nil {
		t.Error("Decryption with wrong passphrase should fail")
	}

	// Decrypt with correct encryptor (should succeed)
	decrypted, err := enc1.Decrypt(encrypted)
	if err != nil {
		t.Errorf("Decryption with correct passphrase failed: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("Decrypted text = %v, want %v", decrypted, plaintext)
	}
}

func TestDecryptInvalid(t *testing.T) {
	enc, err := NewEncryptor("test-passphrase")
	if err != nil {
		t.Fatalf("Failed to create encryptor: %v", err)
	}

	tests := []struct {
		name      string
		encrypted string
	}{
		{
			name:      "Empty string",
			encrypted: "",
		},
		{
			name:      "Invalid base64",
			encrypted: "not-base64!@#$",
		},
		{
			name:      "Valid base64 but too short",
			encrypted: "dGVzdA==", // "test" in base64
		},
		{
			name:      "Valid base64 but not encrypted data",
			encrypted: "dGhpcyBpcyBhIGxvbmdlciB0ZXN0IHN0cmluZyBidXQgbm90IGVuY3J5cHRlZA==",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := enc.Decrypt(tt.encrypted)
			if err == nil {
				t.Error("Decrypt() should fail for invalid input")
			}
		})
	}
}

func TestIsEncrypted(t *testing.T) {
	enc, err := NewEncryptor("test-passphrase")
	if err != nil {
		t.Fatalf("Failed to create encryptor: %v", err)
	}

	// Create an encrypted password
	encrypted, err := enc.Encrypt("password123")
	if err != nil {
		t.Fatalf("Failed to encrypt: %v", err)
	}

	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "Encrypted text",
			text: encrypted,
			want: true,
		},
		{
			name: "Plain text",
			text: "password123",
			want: false,
		},
		{
			name: "Empty string",
			text: "",
			want: false,
		},
		{
			name: "Invalid base64",
			text: "not-base64!@#$",
			want: false,
		},
		{
			name: "Valid base64 but too short",
			text: "dGVzdA==", // "test" in base64
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsEncrypted(tt.text); got != tt.want {
				t.Errorf("IsEncrypted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func BenchmarkEncrypt(b *testing.B) {
	enc, err := NewEncryptor("test-passphrase")
	if err != nil {
		b.Fatalf("Failed to create encryptor: %v", err)
	}

	plaintext := "password123"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := enc.Encrypt(plaintext)
		if err != nil {
			b.Fatalf("Encryption failed: %v", err)
		}
	}
}

func BenchmarkDecrypt(b *testing.B) {
	enc, err := NewEncryptor("test-passphrase")
	if err != nil {
		b.Fatalf("Failed to create encryptor: %v", err)
	}

	plaintext := "password123"
	encrypted, err := enc.Encrypt(plaintext)
	if err != nil {
		b.Fatalf("Failed to encrypt: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := enc.Decrypt(encrypted)
		if err != nil {
			b.Fatalf("Decryption failed: %v", err)
		}
	}
}
