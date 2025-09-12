package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/pbkdf2"
)

const (
	// SaltSize is the size of the salt for key derivation
	SaltSize = 32
	// NonceSize is the size of the nonce for GCM
	NonceSize = 12
	// KeySize is the size of the AES key (256 bits)
	KeySize = 32
	// Iterations for PBKDF2
	Iterations = 100000
)

// Encryptor handles password encryption and decryption
type Encryptor struct {
	key []byte
}

// NewEncryptor creates a new encryptor with the given passphrase
func NewEncryptor(passphrase string) (*Encryptor, error) {
	if passphrase == "" {
		return nil, errors.New("passphrase cannot be empty")
	}

	// Use a fixed salt derived from the passphrase for simplicity
	// In production, you might want to store a random salt
	salt := sha256.Sum256([]byte("shoal-salt-" + passphrase))

	// Derive key using PBKDF2
	key := pbkdf2.Key([]byte(passphrase), salt[:], Iterations, KeySize, sha256.New)

	return &Encryptor{
		key: key,
	}, nil
}

// Encrypt encrypts a plaintext password
func (e *Encryptor) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", errors.New("plaintext cannot be empty")
	}

	// Create AES cipher
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt the plaintext
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)

	// Combine nonce and ciphertext
	combined := make([]byte, len(nonce)+len(ciphertext))
	copy(combined, nonce)
	copy(combined[len(nonce):], ciphertext)

	// Encode to base64 for storage
	return base64.StdEncoding.EncodeToString(combined), nil
}

// Decrypt decrypts an encrypted password
func (e *Encryptor) Decrypt(encrypted string) (string, error) {
	if encrypted == "" {
		return "", errors.New("encrypted text cannot be empty")
	}

	// Decode from base64
	combined, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	// Create AES cipher
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Check minimum length
	if len(combined) < gcm.NonceSize() {
		return "", errors.New("encrypted text too short")
	}

	// Extract nonce and ciphertext
	nonce := combined[:gcm.NonceSize()]
	ciphertext := combined[gcm.NonceSize():]

	// Decrypt the ciphertext
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// IsEncrypted checks if a string appears to be encrypted
// This is a simple heuristic based on base64 encoding and minimum length
func IsEncrypted(s string) bool {
	if s == "" {
		return false
	}

	// Try to decode as base64
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return false
	}

	// Check if it has minimum length for nonce + some ciphertext
	// NonceSize (12) + at least some encrypted data + GCM tag (16)
	return len(decoded) >= NonceSize+16
}
