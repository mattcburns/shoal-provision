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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Storage implements an OCI Image Layout storage backend on the filesystem.
// It provides content-addressed blob storage with deduplication and atomic operations.
type Storage struct {
	root string
	mu   sync.RWMutex
}

// NewStorage creates a new OCI storage backend rooted at the given path.
// The root directory is created if it doesn't exist.
func NewStorage(root string) (*Storage, error) {
	if root == "" {
		return nil, fmt.Errorf("storage root cannot be empty")
	}

	s := &Storage{
		root: root,
	}

	// Create directory structure
	if err := s.init(); err != nil {
		return nil, fmt.Errorf("failed to initialize storage: %w", err)
	}

	return s, nil
}

// init creates the OCI layout directory structure if it doesn't exist.
func (s *Storage) init() error {
	// Create root directory
	if err := os.MkdirAll(s.root, 0755); err != nil {
		return fmt.Errorf("failed to create root directory: %w", err)
	}

	// Create blobs/sha256 directory
	blobDir := filepath.Join(s.root, "blobs", "sha256")
	if err := os.MkdirAll(blobDir, 0755); err != nil {
		return fmt.Errorf("failed to create blob directory: %w", err)
	}

	// Create repositories directory
	repoDir := filepath.Join(s.root, "repositories")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		return fmt.Errorf("failed to create repositories directory: %w", err)
	}

	// Create oci-layout version marker
	layoutPath := filepath.Join(s.root, "oci-layout")
	if _, err := os.Stat(layoutPath); os.IsNotExist(err) {
		layoutContent := `{"imageLayoutVersion":"1.0.0"}`
		if err := os.WriteFile(layoutPath, []byte(layoutContent), 0644); err != nil {
			return fmt.Errorf("failed to create oci-layout: %w", err)
		}
	}

	return nil
}

// BlobPath returns the filesystem path for a blob with the given digest.
// The digest should be in the format "sha256:hexhexhex".
func (s *Storage) BlobPath(digest string) (string, error) {
	// Parse digest format (sha256:hexhexhex)
	if len(digest) < 8 || digest[:7] != "sha256:" {
		return "", fmt.Errorf("invalid digest format: %s", digest)
	}

	hex := digest[7:]
	if len(hex) != 64 {
		return "", fmt.Errorf("invalid sha256 digest length: %s", digest)
	}

	return filepath.Join(s.root, "blobs", "sha256", hex), nil
}

// BlobExists checks if a blob with the given digest exists in storage.
func (s *Storage) BlobExists(digest string) (bool, error) {
	path, err := s.BlobPath(digest)
	if err != nil {
		return false, err
	}

	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("failed to check blob existence: %w", err)
}

// BlobSize returns the size in bytes of the blob with the given digest.
// Returns an error if the blob doesn't exist.
func (s *Storage) BlobSize(digest string) (int64, error) {
	path, err := s.BlobPath(digest)
	if err != nil {
		return 0, err
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("blob not found: %s", digest)
		}
		return 0, fmt.Errorf("failed to stat blob: %w", err)
	}

	return info.Size(), nil
}

// ReadBlob opens a blob for reading and returns a ReadCloser.
// The caller is responsible for closing the reader.
func (s *Storage) ReadBlob(digest string) (io.ReadCloser, error) {
	path, err := s.BlobPath(digest)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("blob not found: %s", digest)
		}
		return nil, fmt.Errorf("failed to open blob: %w", err)
	}

	return f, nil
}

// WriteBlob writes a blob to storage with atomic rename.
// It computes the sha256 digest of the content and verifies it matches expectedDigest.
// Returns the actual digest and any error.
func (s *Storage) WriteBlob(r io.Reader, expectedDigest string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create temporary file
	tmpDir := filepath.Join(s.root, "tmp")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create tmp directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(tmpDir, "blob-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Cleanup temp file on error
	defer func() {
		if tmpFile != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
		}
	}()

	// Compute digest while writing
	hash := sha256.New()
	tee := io.TeeReader(r, hash)

	if _, err := io.Copy(tmpFile, tee); err != nil {
		return "", fmt.Errorf("failed to write blob data: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		return "", fmt.Errorf("failed to sync blob data: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}
	tmpFile = nil // Prevent deferred cleanup

	// Compute final digest
	digestHex := hex.EncodeToString(hash.Sum(nil))
	actualDigest := "sha256:" + digestHex

	// Verify digest if expected digest was provided
	if expectedDigest != "" && actualDigest != expectedDigest {
		os.Remove(tmpPath)
		return "", fmt.Errorf("digest mismatch: expected %s, got %s", expectedDigest, actualDigest)
	}

	// Move to final location (atomic rename)
	finalPath, err := s.BlobPath(actualDigest)
	if err != nil {
		os.Remove(tmpPath)
		return "", err
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(finalPath), 0755); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to create blob directory: %w", err)
	}

	// Check if blob already exists (deduplication)
	if _, err := os.Stat(finalPath); err == nil {
		os.Remove(tmpPath)
		return actualDigest, nil
	}

	// Atomic rename
	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to rename temp file: %w", err)
	}

	return actualDigest, nil
}

// DeleteBlob removes a blob from storage.
// This is typically called by the garbage collector.
func (s *Storage) DeleteBlob(digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.BlobPath(digest)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete blob: %w", err)
	}

	return nil
}

// RepositoryPath returns the filesystem path for a repository.
func (s *Storage) RepositoryPath(name string) string {
	return filepath.Join(s.root, "repositories", name)
}

// HealthCheck verifies that storage is accessible and writable.
// Returns nil if healthy, error otherwise.
func (s *Storage) HealthCheck() error {
	// Check if root directory exists
	if _, err := os.Stat(s.root); err != nil {
		return fmt.Errorf("storage root not accessible: %w", err)
	}

	// Check if oci-layout marker exists
	layoutPath := filepath.Join(s.root, "oci-layout")
	if _, err := os.Stat(layoutPath); err != nil {
		return fmt.Errorf("oci-layout marker not found: %w", err)
	}

	// Check if blob directory is writable by creating and removing a test file
	testDir := filepath.Join(s.root, "tmp")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		return fmt.Errorf("tmp directory not writable: %w", err)
	}

	testFile, err := os.CreateTemp(testDir, "healthcheck-*")
	if err != nil {
		return fmt.Errorf("storage not writable: %w", err)
	}
	testPath := testFile.Name()
	testFile.Close()
	os.Remove(testPath)

	return nil
}
