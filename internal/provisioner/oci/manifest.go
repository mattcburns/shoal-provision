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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Manifest represents an OCI manifest with minimal validation.
type Manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType,omitempty"`
	Config        *Descriptor       `json:"config,omitempty"`
	Layers        []Descriptor      `json:"layers,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
	Subject       *Descriptor       `json:"subject,omitempty"`
	ArtifactType  string            `json:"artifactType,omitempty"`
	Blobs         []Descriptor      `json:"blobs,omitempty"`
	Raw           json.RawMessage   `json:"-"`
}

// Descriptor represents an OCI descriptor.
type Descriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// PutManifest stores a manifest for a repository at the given reference (tag or digest).
// The manifest content is validated minimally and stored as raw JSON.
// Returns the computed digest of the manifest.
func (s *Storage) PutManifest(repo, reference string, manifestData []byte) (string, error) {
	// Validate manifest JSON structure
	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return "", fmt.Errorf("invalid manifest JSON: %w", err)
	}

	// Basic validation
	if manifest.SchemaVersion == 0 {
		return "", fmt.Errorf("manifest missing schemaVersion")
	}

	// Compute manifest digest
	hash := sha256.Sum256(manifestData)
	manifestDigest := "sha256:" + hex.EncodeToString(hash[:])

	// Store manifest as a blob
	if _, err := s.WriteBlob(strings.NewReader(string(manifestData)), manifestDigest); err != nil {
		return "", fmt.Errorf("failed to write manifest blob: %w", err)
	}

	// If reference is a tag, create/update tag reference
	if !strings.HasPrefix(reference, "sha256:") {
		if err := s.PutTag(repo, reference, manifestDigest); err != nil {
			return "", fmt.Errorf("failed to update tag: %w", err)
		}
	}

	return manifestDigest, nil
}

// GetManifest retrieves a manifest by reference (tag or digest).
// Returns the manifest data and its digest.
func (s *Storage) GetManifest(repo, reference string) ([]byte, string, error) {
	var digest string

	// Resolve tag to digest if reference is not a digest
	if strings.HasPrefix(reference, "sha256:") {
		digest = reference
	} else {
		var err error
		digest, err = s.GetTag(repo, reference)
		if err != nil {
			return nil, "", fmt.Errorf("failed to resolve tag: %w", err)
		}
	}

	// Read manifest blob
	r, err := s.ReadBlob(digest)
	if err != nil {
		return nil, "", fmt.Errorf("manifest not found: %w", err)
	}
	defer r.Close()

	// Read manifest data
	var buf strings.Builder
	if _, err := io.Copy(&buf, r); err != nil {
		return nil, "", fmt.Errorf("failed to read manifest: %w", err)
	}

	return []byte(buf.String()), digest, nil
}

// ManifestExists checks if a manifest exists for the given reference.
func (s *Storage) ManifestExists(repo, reference string) (bool, error) {
	var digest string

	// Resolve tag to digest if reference is not a digest
	if strings.HasPrefix(reference, "sha256:") {
		digest = reference
	} else {
		var err error
		digest, err = s.GetTag(repo, reference)
		if err != nil {
			// Tag doesn't exist
			if strings.Contains(err.Error(), "not found") {
				return false, nil
			}
			return false, err
		}
	}

	// Check if manifest blob exists
	return s.BlobExists(digest)
}

// DeleteManifest removes a manifest reference (tag).
// This is a soft delete - it only removes the tag pointer, not the manifest blob.
// The blob will be removed by garbage collection if unreferenced.
func (s *Storage) DeleteManifest(repo, reference string) error {
	// Only allow deletion of tags, not digests
	if strings.HasPrefix(reference, "sha256:") {
		return fmt.Errorf("cannot delete manifest by digest, only tags can be deleted")
	}

	return s.DeleteTag(repo, reference)
}

// PutTag creates or updates a tag reference to point to a manifest digest.
func (s *Storage) PutTag(repo, tag, digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate digest format
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != 71 {
		return fmt.Errorf("invalid digest format: %s", digest)
	}

	// Validate tag name (basic check)
	if tag == "" || strings.Contains(tag, "/") || strings.Contains(tag, "..") {
		return fmt.Errorf("invalid tag name: %s", tag)
	}

	// Create repository refs directory
	refsDir := filepath.Join(s.RepositoryPath(repo), "refs")
	if err := os.MkdirAll(refsDir, 0755); err != nil {
		return fmt.Errorf("failed to create refs directory: %w", err)
	}

	// Write tag file pointing to digest
	tagPath := filepath.Join(refsDir, tag)
	if err := os.WriteFile(tagPath, []byte(digest), 0644); err != nil {
		return fmt.Errorf("failed to write tag file: %w", err)
	}

	return nil
}

// GetTag resolves a tag to a manifest digest.
func (s *Storage) GetTag(repo, tag string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tagPath := filepath.Join(s.RepositoryPath(repo), "refs", tag)
	data, err := os.ReadFile(tagPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("tag not found: %s", tag)
		}
		return "", fmt.Errorf("failed to read tag: %w", err)
	}

	digest := strings.TrimSpace(string(data))
	return digest, nil
}

// DeleteTag removes a tag reference.
func (s *Storage) DeleteTag(repo, tag string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tagPath := filepath.Join(s.RepositoryPath(repo), "refs", tag)
	if err := os.Remove(tagPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("tag not found: %s", tag)
		}
		return fmt.Errorf("failed to delete tag: %w", err)
	}

	return nil
}

// ListTags returns all tags for a repository.
func (s *Storage) ListTags(repo string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	refsDir := filepath.Join(s.RepositoryPath(repo), "refs")
	entries, err := os.ReadDir(refsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read refs directory: %w", err)
	}

	tags := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			tags = append(tags, entry.Name())
		}
	}

	return tags, nil
}
