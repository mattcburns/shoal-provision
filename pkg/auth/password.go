package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const (
	// DefaultCost is the default bcrypt cost parameter
	DefaultCost = 12
)

// HashPassword hashes a plaintext password using bcrypt
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("password cannot be empty")
	}

	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}

	return string(hashedBytes), nil
}

// VerifyPassword verifies a plaintext password against a hashed password
func VerifyPassword(password, hash string) error {
	if password == "" || hash == "" {
		return fmt.Errorf("password and hash cannot be empty")
	}

	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil {
		if err == bcrypt.ErrMismatchedHashAndPassword {
			return fmt.Errorf("invalid password")
		}
		return fmt.Errorf("failed to verify password: %w", err)
	}

	return nil
}

// IsHashed checks if a string appears to be a bcrypt hash
func IsHashed(s string) bool {
	// Bcrypt hashes start with $2a$, $2b$, or $2y$ followed by cost parameter
	if len(s) < 60 {
		return false
	}

	// Check for bcrypt prefix
	return (s[0] == '$' && s[1] == '2' &&
		(s[2] == 'a' || s[2] == 'b' || s[2] == 'y') &&
		s[3] == '$')
}
