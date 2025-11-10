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
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// HashAlgorithm represents the password hashing algorithm.
type HashAlgorithm string

const (
	// Argon2id is the preferred password hashing algorithm.
	Argon2id HashAlgorithm = "argon2id"
	// Bcrypt is the fallback password hashing algorithm.
	Bcrypt HashAlgorithm = "bcrypt"
)

// Argon2Params defines the parameters for Argon2id hashing.
type Argon2Params struct {
	Memory      uint32 // Memory in KiB (default: 64 MiB)
	Iterations  uint32 // Time cost (default: 3)
	Parallelism uint8  // Number of threads (default: 2)
	SaltLength  uint32 // Salt length in bytes (default: 16)
	KeyLength   uint32 // Output key length (default: 32)
}

// DefaultArgon2Params returns recommended Argon2id parameters.
// Based on OWASP recommendations for modern systems (2023).
func DefaultArgon2Params() Argon2Params {
	return Argon2Params{
		Memory:      64 * 1024, // 64 MiB
		Iterations:  3,
		Parallelism: 2,
		SaltLength:  16,
		KeyLength:   32,
	}
}

var (
	// ErrInvalidHash indicates the hash format is invalid.
	ErrInvalidHash = errors.New("invalid hash format")
	// ErrIncompatibleVersion indicates unsupported Argon2 version.
	ErrIncompatibleVersion = errors.New("incompatible argon2 version")
)

// HashPassword hashes a password using Argon2id with default parameters.
// Returns a string in PHC format:
// $argon2id$v=19$m=65536,t=3,p=2$SALT$HASH
func HashPassword(password string) (string, error) {
	return HashPasswordWithParams(password, DefaultArgon2Params())
}

// HashPasswordWithParams hashes a password using Argon2id with custom parameters.
func HashPasswordWithParams(password string, params Argon2Params) (string, error) {
	// Generate random salt
	salt := make([]byte, params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	// Hash password
	hash := argon2.IDKey(
		[]byte(password),
		salt,
		params.Iterations,
		params.Memory,
		params.Parallelism,
		params.KeyLength,
	)

	// Encode in PHC string format
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		params.Memory,
		params.Iterations,
		params.Parallelism,
		b64Salt,
		b64Hash,
	), nil
}

// VerifyPassword checks if a password matches a hash.
// Supports both Argon2id and bcrypt hashes.
func VerifyPassword(password, hash string) (bool, error) {
	if strings.HasPrefix(hash, "$argon2id$") {
		return verifyArgon2id(password, hash)
	} else if strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$") {
		return verifyBcrypt(password, hash)
	}
	return false, ErrInvalidHash
}

// verifyArgon2id verifies a password against an Argon2id hash.
func verifyArgon2id(password, encodedHash string) (bool, error) {
	params, salt, hash, err := decodeArgon2Hash(encodedHash)
	if err != nil {
		return false, err
	}

	// Hash the password with the same parameters
	otherHash := argon2.IDKey(
		[]byte(password),
		salt,
		params.Iterations,
		params.Memory,
		params.Parallelism,
		params.KeyLength,
	)

	// Constant-time comparison
	if subtle.ConstantTimeCompare(hash, otherHash) == 1 {
		return true, nil
	}
	return false, nil
}

// verifyBcrypt verifies a password against a bcrypt hash.
func verifyBcrypt(password, hash string) (bool, error) {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return false, nil
	}
	return false, err
}

// decodeArgon2Hash parses an Argon2id PHC format hash string.
func decodeArgon2Hash(encodedHash string) (params Argon2Params, salt, hash []byte, err error) {
	// Format: $argon2id$v=19$m=65536,t=3,p=2$SALT$HASH
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return params, nil, nil, ErrInvalidHash
	}

	if parts[1] != "argon2id" {
		return params, nil, nil, ErrInvalidHash
	}

	// Parse version
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return params, nil, nil, err
	}
	if version != argon2.Version {
		return params, nil, nil, ErrIncompatibleVersion
	}

	// Parse parameters
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d",
		&params.Memory,
		&params.Iterations,
		&params.Parallelism,
	); err != nil {
		return params, nil, nil, err
	}

	// Decode salt
	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return params, nil, nil, err
	}
	params.SaltLength = uint32(len(salt))

	// Decode hash
	hash, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return params, nil, nil, err
	}
	params.KeyLength = uint32(len(hash))

	return params, salt, hash, nil
}

// HashPasswordBcrypt hashes a password using bcrypt (fallback algorithm).
// Cost is set to bcrypt.DefaultCost (10).
func HashPasswordBcrypt(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt hash failed: %w", err)
	}
	return string(hash), nil
}

// NeedsRehash checks if a hash needs to be upgraded to current parameters.
// Returns true if the hash uses bcrypt or outdated Argon2id parameters.
func NeedsRehash(hash string) bool {
	if strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$") {
		// Bcrypt should be upgraded to Argon2id
		return true
	}

	if !strings.HasPrefix(hash, "$argon2id$") {
		return false
	}

	params, _, _, err := decodeArgon2Hash(hash)
	if err != nil {
		return false
	}

	defaults := DefaultArgon2Params()
	// Upgrade if parameters are weaker than defaults
	return params.Memory < defaults.Memory ||
		params.Iterations < defaults.Iterations ||
		params.Parallelism < defaults.Parallelism ||
		params.KeyLength < defaults.KeyLength
}
