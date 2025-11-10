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

func TestHashPassword_Argon2id(t *testing.T) {
	password := "test-password-123"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	// Verify format
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("hash should start with $argon2id$, got: %s", hash)
	}

	// Hash should be different each time due to random salt
	hash2, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if hash == hash2 {
		t.Error("two hashes of same password should differ (different salts)")
	}

	// Verify password
	ok, err := VerifyPassword(password, hash)
	if err != nil {
		t.Fatalf("VerifyPassword failed: %v", err)
	}
	if !ok {
		t.Error("password verification failed")
	}
}

func TestVerifyPassword_CorrectPassword(t *testing.T) {
	password := "correct-password"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := VerifyPassword(password, hash)
	if err != nil {
		t.Fatalf("VerifyPassword failed: %v", err)
	}
	if !ok {
		t.Error("expected password to match")
	}
}

func TestVerifyPassword_WrongPassword(t *testing.T) {
	password := "correct-password"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := VerifyPassword("wrong-password", hash)
	if err != nil {
		t.Fatalf("VerifyPassword failed: %v", err)
	}
	if ok {
		t.Error("expected password not to match")
	}
}

func TestVerifyPassword_Bcrypt(t *testing.T) {
	password := "bcrypt-password"
	hash, err := HashPasswordBcrypt(password)
	if err != nil {
		t.Fatal(err)
	}

	// Verify bcrypt hash format
	if !strings.HasPrefix(hash, "$2a$") && !strings.HasPrefix(hash, "$2b$") {
		t.Errorf("bcrypt hash should start with $2a$ or $2b$, got: %s", hash)
	}

	// Verify correct password
	ok, err := VerifyPassword(password, hash)
	if err != nil {
		t.Fatalf("VerifyPassword failed: %v", err)
	}
	if !ok {
		t.Error("expected bcrypt password to match")
	}

	// Verify wrong password
	ok, err = VerifyPassword("wrong", hash)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected wrong password not to match")
	}
}

func TestVerifyPassword_InvalidHash(t *testing.T) {
	tests := []struct {
		name string
		hash string
	}{
		{"empty", ""},
		{"random string", "not-a-hash"},
		{"invalid prefix", "$unknown$hash"},
		{"truncated argon2id", "$argon2id$v=19"},
		{"malformed", "$argon2id$v=19$m=invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := VerifyPassword("password", tt.hash)
			if err == nil {
				t.Error("expected error for invalid hash")
			}
			if ok {
				t.Error("expected verification to fail")
			}
		})
	}
}

func TestHashPasswordWithParams_CustomParams(t *testing.T) {
	password := "test-password"
	params := Argon2Params{
		Memory:      32 * 1024, // 32 MiB
		Iterations:  2,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	}

	hash, err := HashPasswordWithParams(password, params)
	if err != nil {
		t.Fatalf("HashPasswordWithParams failed: %v", err)
	}

	// Verify format includes custom parameters
	if !strings.Contains(hash, "m=32768") {
		t.Errorf("hash should contain custom memory parameter, got: %s", hash)
	}
	if !strings.Contains(hash, "t=2") {
		t.Errorf("hash should contain custom iterations, got: %s", hash)
	}

	// Verify password
	ok, err := VerifyPassword(password, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("password verification with custom params failed")
	}
}

func TestDecodeArgon2Hash(t *testing.T) {
	hash := "$argon2id$v=19$m=65536,t=3,p=2$c2FsdDEyMzQ1Njc4OTBhYg$aGFzaDEyMzQ1Njc4OTBhYmNkZWZnaGlqa2xtbm8"

	params, salt, hashBytes, err := decodeArgon2Hash(hash)
	if err != nil {
		t.Fatalf("decodeArgon2Hash failed: %v", err)
	}

	if params.Memory != 65536 {
		t.Errorf("expected memory=65536, got %d", params.Memory)
	}
	if params.Iterations != 3 {
		t.Errorf("expected iterations=3, got %d", params.Iterations)
	}
	if params.Parallelism != 2 {
		t.Errorf("expected parallelism=2, got %d", params.Parallelism)
	}
	if len(salt) == 0 {
		t.Error("salt should not be empty")
	}
	if len(hashBytes) == 0 {
		t.Error("hash bytes should not be empty")
	}
}

func TestNeedsRehash_Bcrypt(t *testing.T) {
	bcryptHash := "$2a$10$abcdefghijklmnopqrstuv1234567890123456789012"
	if !NeedsRehash(bcryptHash) {
		t.Error("bcrypt hash should need rehash to argon2id")
	}
}

func TestNeedsRehash_WeakArgon2id(t *testing.T) {
	// Hash with weaker parameters than defaults
	weakParams := Argon2Params{
		Memory:      16 * 1024, // 16 MiB (weaker than default 64 MiB)
		Iterations:  1,         // weaker than default 3
		Parallelism: 1,         // weaker than default 2
		SaltLength:  16,
		KeyLength:   32,
	}

	hash, err := HashPasswordWithParams("password", weakParams)
	if err != nil {
		t.Fatal(err)
	}

	if !NeedsRehash(hash) {
		t.Error("weak argon2id hash should need rehash")
	}
}

func TestNeedsRehash_CurrentParams(t *testing.T) {
	hash, err := HashPassword("password")
	if err != nil {
		t.Fatal(err)
	}

	if NeedsRehash(hash) {
		t.Error("hash with current default params should not need rehash")
	}
}

func TestPasswordHashRoundtrip(t *testing.T) {
	passwords := []string{
		"simple",
		"with spaces and symbols !@#$%",
		"unicode-παράδειγμα-例-🔐",
		strings.Repeat("a", 256), // long password
	}

	for _, pwd := range passwords {
		t.Run(pwd[:min(20, len(pwd))], func(t *testing.T) {
			hash, err := HashPassword(pwd)
			if err != nil {
				t.Fatalf("HashPassword failed: %v", err)
			}

			ok, err := VerifyPassword(pwd, hash)
			if err != nil {
				t.Fatalf("VerifyPassword failed: %v", err)
			}
			if !ok {
				t.Error("roundtrip verification failed")
			}

			// Verify wrong password fails
			ok, err = VerifyPassword(pwd+"wrong", hash)
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				t.Error("wrong password should not verify")
			}
		})
	}
}

func TestDefaultArgon2Params(t *testing.T) {
	params := DefaultArgon2Params()

	if params.Memory < 64*1024 {
		t.Errorf("memory should be at least 64 MiB, got %d KiB", params.Memory)
	}
	if params.Iterations < 2 {
		t.Errorf("iterations should be at least 2, got %d", params.Iterations)
	}
	if params.Parallelism < 1 {
		t.Errorf("parallelism should be at least 1, got %d", params.Parallelism)
	}
	if params.SaltLength < 16 {
		t.Errorf("salt length should be at least 16, got %d", params.SaltLength)
	}
	if params.KeyLength < 32 {
		t.Errorf("key length should be at least 32, got %d", params.KeyLength)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
