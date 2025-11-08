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
	"sync"
	"testing"
)

func TestPutAndGetManifest(t *testing.T) {
	tmpDir := t.TempDir()
	s, _ := NewStorage(tmpDir)

	repo := "test/repo"
	tag := "v1.0"

	manifest := map[string]interface{}{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]interface{}{
			"mediaType": "application/vnd.oci.image.config.v1+json",
			"digest":    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			"size":      1024,
		},
		"layers": []map[string]interface{}{
			{
				"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
				"digest":    "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
				"size":      2048,
			},
		},
	}

	manifestData, _ := json.Marshal(manifest)

	t.Run("put and get manifest by tag", func(t *testing.T) {
		digest, err := s.PutManifest(repo, tag, manifestData)
		if err != nil {
			t.Fatalf("PutManifest failed: %v", err)
		}

		// Verify digest format
		if len(digest) != 71 || digest[:7] != "sha256:" {
			t.Fatalf("invalid digest format: %s", digest)
		}

		// Get manifest by tag
		retrieved, retrievedDigest, err := s.GetManifest(repo, tag)
		if err != nil {
			t.Fatalf("GetManifest by tag failed: %v", err)
		}

		if retrievedDigest != digest {
			t.Fatalf("digest mismatch: expected %s, got %s", digest, retrievedDigest)
		}

		// Verify content
		var retrievedManifest map[string]interface{}
		if err := json.Unmarshal(retrieved, &retrievedManifest); err != nil {
			t.Fatalf("retrieved manifest is not valid JSON: %v", err)
		}

		if retrievedManifest["schemaVersion"].(float64) != 2 {
			t.Fatal("schemaVersion mismatch")
		}
	})

	t.Run("get manifest by digest", func(t *testing.T) {
		// Put manifest first
		digest, _ := s.PutManifest(repo, "v2.0", manifestData)

		// Get by digest
		retrieved, retrievedDigest, err := s.GetManifest(repo, digest)
		if err != nil {
			t.Fatalf("GetManifest by digest failed: %v", err)
		}

		if retrievedDigest != digest {
			t.Fatalf("digest mismatch: expected %s, got %s", digest, retrievedDigest)
		}

		if string(retrieved) != string(manifestData) {
			t.Fatal("retrieved manifest content doesn't match original")
		}
	})

	t.Run("invalid manifest JSON", func(t *testing.T) {
		invalidJSON := []byte("{invalid json")
		_, err := s.PutManifest(repo, "bad", invalidJSON)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("manifest missing schemaVersion", func(t *testing.T) {
		invalidManifest := map[string]interface{}{
			"mediaType": "application/vnd.oci.image.manifest.v1+json",
		}
		invalidData, _ := json.Marshal(invalidManifest)

		_, err := s.PutManifest(repo, "invalid", invalidData)
		if err == nil {
			t.Fatal("expected error for missing schemaVersion")
		}
	})

	t.Run("update existing tag", func(t *testing.T) {
		tag := "latest"

		// Put first manifest
		manifest1 := map[string]interface{}{
			"schemaVersion": 2,
			"mediaType":     "application/vnd.oci.image.manifest.v1+json",
			"config": map[string]interface{}{
				"digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				"size":   100,
			},
		}
		data1, _ := json.Marshal(manifest1)
		digest1, _ := s.PutManifest(repo, tag, data1)

		// Put second manifest with same tag
		manifest2 := map[string]interface{}{
			"schemaVersion": 2,
			"mediaType":     "application/vnd.oci.image.manifest.v1+json",
			"config": map[string]interface{}{
				"digest": "sha256:2222222222222222222222222222222222222222222222222222222222222222",
				"size":   200,
			},
		}
		data2, _ := json.Marshal(manifest2)
		digest2, _ := s.PutManifest(repo, tag, data2)

		if digest1 == digest2 {
			t.Fatal("expected different digests for different manifests")
		}

		// Verify tag points to second manifest
		retrieved, retrievedDigest, _ := s.GetManifest(repo, tag)
		if retrievedDigest != digest2 {
			t.Fatalf("tag should point to second manifest, got %s expected %s", retrievedDigest, digest2)
		}

		if string(retrieved) != string(data2) {
			t.Fatal("tag should resolve to second manifest content")
		}
	})
}

func TestManifestExists(t *testing.T) {
	tmpDir := t.TempDir()
	s, _ := NewStorage(tmpDir)

	repo := "test/repo"
	tag := "exists"

	manifest := map[string]interface{}{
		"schemaVersion": 2,
		"config": map[string]interface{}{
			"digest": "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			"size":   100,
		},
	}
	manifestData, _ := json.Marshal(manifest)
	digest, _ := s.PutManifest(repo, tag, manifestData)

	t.Run("manifest exists by tag", func(t *testing.T) {
		exists, err := s.ManifestExists(repo, tag)
		if err != nil {
			t.Fatalf("ManifestExists failed: %v", err)
		}
		if !exists {
			t.Fatal("expected manifest to exist")
		}
	})

	t.Run("manifest exists by digest", func(t *testing.T) {
		exists, err := s.ManifestExists(repo, digest)
		if err != nil {
			t.Fatalf("ManifestExists failed: %v", err)
		}
		if !exists {
			t.Fatal("expected manifest to exist")
		}
	})

	t.Run("manifest doesn't exist", func(t *testing.T) {
		exists, err := s.ManifestExists(repo, "nonexistent")
		if err != nil {
			t.Fatalf("ManifestExists failed: %v", err)
		}
		if exists {
			t.Fatal("expected manifest to not exist")
		}
	})
}

func TestDeleteManifest(t *testing.T) {
	tmpDir := t.TempDir()
	s, _ := NewStorage(tmpDir)

	repo := "test/repo"
	tag := "delete-me"

	manifest := map[string]interface{}{
		"schemaVersion": 2,
		"config": map[string]interface{}{
			"digest": "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			"size":   100,
		},
	}
	manifestData, _ := json.Marshal(manifest)
	digest, _ := s.PutManifest(repo, tag, manifestData)

	t.Run("delete tag", func(t *testing.T) {
		err := s.DeleteManifest(repo, tag)
		if err != nil {
			t.Fatalf("DeleteManifest failed: %v", err)
		}

		// Verify tag is gone
		exists, _ := s.ManifestExists(repo, tag)
		if exists {
			t.Fatal("tag should not exist after deletion")
		}

		// Verify manifest blob still exists (soft delete)
		blobExists, _ := s.BlobExists(digest)
		if !blobExists {
			t.Fatal("manifest blob should still exist after tag deletion")
		}
	})

	t.Run("cannot delete by digest", func(t *testing.T) {
		err := s.DeleteManifest(repo, digest)
		if err == nil {
			t.Fatal("expected error when deleting by digest")
		}
	})

	t.Run("delete non-existent tag", func(t *testing.T) {
		err := s.DeleteManifest(repo, "nonexistent")
		if err == nil {
			t.Fatal("expected error for non-existent tag")
		}
	})
}

func TestTagOperations(t *testing.T) {
	tmpDir := t.TempDir()
	s, _ := NewStorage(tmpDir)

	repo := "test/repo"
	tag := "v1.0"
	digest := "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"

	t.Run("put and get tag", func(t *testing.T) {
		err := s.PutTag(repo, tag, digest)
		if err != nil {
			t.Fatalf("PutTag failed: %v", err)
		}

		retrieved, err := s.GetTag(repo, tag)
		if err != nil {
			t.Fatalf("GetTag failed: %v", err)
		}

		if retrieved != digest {
			t.Fatalf("expected digest %s, got %s", digest, retrieved)
		}
	})

	t.Run("invalid digest format", func(t *testing.T) {
		err := s.PutTag(repo, "bad", "invalid-digest")
		if err == nil {
			t.Fatal("expected error for invalid digest")
		}
	})

	t.Run("invalid tag name", func(t *testing.T) {
		tests := []string{"", "tag/with/slash", "tag..with..dots"}
		for _, badTag := range tests {
			err := s.PutTag(repo, badTag, digest)
			if err == nil {
				t.Fatalf("expected error for invalid tag: %s", badTag)
			}
		}
	})

	t.Run("get non-existent tag", func(t *testing.T) {
		_, err := s.GetTag(repo, "nonexistent")
		if err == nil {
			t.Fatal("expected error for non-existent tag")
		}
	})

	t.Run("delete tag", func(t *testing.T) {
		s.PutTag(repo, "temp", digest)
		err := s.DeleteTag(repo, "temp")
		if err != nil {
			t.Fatalf("DeleteTag failed: %v", err)
		}

		_, err = s.GetTag(repo, "temp")
		if err == nil {
			t.Fatal("tag should not exist after deletion")
		}
	})
}

func TestListTags(t *testing.T) {
	tmpDir := t.TempDir()
	s, _ := NewStorage(tmpDir)

	repo := "test/repo"
	digest := "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"

	t.Run("empty repository", func(t *testing.T) {
		tags, err := s.ListTags(repo)
		if err != nil {
			t.Fatalf("ListTags failed: %v", err)
		}
		if len(tags) != 0 {
			t.Fatalf("expected 0 tags, got %d", len(tags))
		}
	})

	t.Run("list multiple tags", func(t *testing.T) {
		expectedTags := []string{"v1.0", "v2.0", "latest"}
		for _, tag := range expectedTags {
			s.PutTag(repo, tag, digest)
		}

		tags, err := s.ListTags(repo)
		if err != nil {
			t.Fatalf("ListTags failed: %v", err)
		}

		if len(tags) != len(expectedTags) {
			t.Fatalf("expected %d tags, got %d", len(expectedTags), len(tags))
		}

		// Verify all tags are present
		tagMap := make(map[string]bool)
		for _, tag := range tags {
			tagMap[tag] = true
		}

		for _, expected := range expectedTags {
			if !tagMap[expected] {
				t.Fatalf("expected tag %s not found", expected)
			}
		}
	})

	t.Run("list after deletion", func(t *testing.T) {
		s.PutTag(repo, "delete-me", digest)
		tags1, _ := s.ListTags(repo)
		initialCount := len(tags1)

		s.DeleteTag(repo, "delete-me")
		tags2, _ := s.ListTags(repo)

		if len(tags2) != initialCount-1 {
			t.Fatalf("expected %d tags after deletion, got %d", initialCount-1, len(tags2))
		}
	})
}

func TestConcurrentTagUpdates(t *testing.T) {
	tmpDir := t.TempDir()
	s, _ := NewStorage(tmpDir)

	repo := "test/repo"
	tag := "concurrent"

	const numGoroutines = 10
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Create unique digest for each goroutine
			digest := fmt.Sprintf("sha256:%064d", id)

			err := s.PutTag(repo, tag, digest)
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: %w", id, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent update error: %v", err)
	}

	// Verify tag exists and points to one of the digests
	resolvedDigest, err := s.GetTag(repo, tag)
	if err != nil {
		t.Fatalf("tag should exist after concurrent updates: %v", err)
	}

	if len(resolvedDigest) != 71 || resolvedDigest[:7] != "sha256:" {
		t.Fatalf("invalid digest format after concurrent updates: %s", resolvedDigest)
	}
}

func TestManifestDigestComputation(t *testing.T) {
	tmpDir := t.TempDir()
	s, _ := NewStorage(tmpDir)

	repo := "test/repo"
	tag := "digest-test"

	manifest := map[string]interface{}{
		"schemaVersion": 2,
		"config": map[string]interface{}{
			"digest": "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			"size":   100,
		},
	}
	manifestData, _ := json.Marshal(manifest)

	// Compute expected digest manually
	hash := sha256.Sum256(manifestData)
	expectedDigest := "sha256:" + hex.EncodeToString(hash[:])

	t.Run("digest matches manual computation", func(t *testing.T) {
		digest, err := s.PutManifest(repo, tag, manifestData)
		if err != nil {
			t.Fatalf("PutManifest failed: %v", err)
		}

		if digest != expectedDigest {
			t.Fatalf("digest mismatch: expected %s, got %s", expectedDigest, digest)
		}
	})

	t.Run("same content produces same digest", func(t *testing.T) {
		digest1, _ := s.PutManifest(repo, "tag1", manifestData)
		digest2, _ := s.PutManifest(repo, "tag2", manifestData)

		if digest1 != digest2 {
			t.Fatal("same manifest content should produce same digest")
		}
	})
}
