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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestNewStorage(t *testing.T) {
	t.Run("creates storage with valid root", func(t *testing.T) {
		tmpDir := t.TempDir()
		s, err := NewStorage(tmpDir)
		if err != nil {
			t.Fatalf("NewStorage failed: %v", err)
		}
		if s == nil {
			t.Fatal("expected non-nil storage")
		}

		// Verify directory structure
		checkDir(t, filepath.Join(tmpDir, "blobs", "sha256"))
		checkDir(t, filepath.Join(tmpDir, "repositories"))
		checkFile(t, filepath.Join(tmpDir, "oci-layout"))
	})

	t.Run("fails with empty root", func(t *testing.T) {
		_, err := NewStorage("")
		if err == nil {
			t.Fatal("expected error for empty root")
		}
	})

	t.Run("idempotent initialization", func(t *testing.T) {
		tmpDir := t.TempDir()
		s1, err := NewStorage(tmpDir)
		if err != nil {
			t.Fatalf("first NewStorage failed: %v", err)
		}

		s2, err := NewStorage(tmpDir)
		if err != nil {
			t.Fatalf("second NewStorage failed: %v", err)
		}

		if s1.root != s2.root {
			t.Fatal("expected same root path")
		}
	})
}

func TestBlobPath(t *testing.T) {
	tmpDir := t.TempDir()
	s, _ := NewStorage(tmpDir)

	t.Run("valid sha256 digest", func(t *testing.T) {
		digest := "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
		path, err := s.BlobPath(digest)
		if err != nil {
			t.Fatalf("BlobPath failed: %v", err)
		}

		expected := filepath.Join(tmpDir, "blobs", "sha256", "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
		if path != expected {
			t.Fatalf("expected path %s, got %s", expected, path)
		}
	})

	t.Run("invalid digest format", func(t *testing.T) {
		_, err := s.BlobPath("invalid")
		if err == nil {
			t.Fatal("expected error for invalid digest")
		}
	})

	t.Run("wrong algorithm", func(t *testing.T) {
		_, err := s.BlobPath("md5:1234567890abcdef")
		if err == nil {
			t.Fatal("expected error for wrong algorithm")
		}
	})

	t.Run("invalid sha256 length", func(t *testing.T) {
		_, err := s.BlobPath("sha256:short")
		if err == nil {
			t.Fatal("expected error for short digest")
		}
	})
}

func TestWriteAndReadBlob(t *testing.T) {
	tmpDir := t.TempDir()
	s, _ := NewStorage(tmpDir)

	t.Run("write and read small blob", func(t *testing.T) {
		content := []byte("hello world")
		expectedDigest := computeDigest(content)

		digest, err := s.WriteBlob(bytes.NewReader(content), expectedDigest)
		if err != nil {
			t.Fatalf("WriteBlob failed: %v", err)
		}
		if digest != expectedDigest {
			t.Fatalf("expected digest %s, got %s", expectedDigest, digest)
		}

		// Read back
		r, err := s.ReadBlob(digest)
		if err != nil {
			t.Fatalf("ReadBlob failed: %v", err)
		}
		defer r.Close()

		readContent, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("reading blob failed: %v", err)
		}

		if !bytes.Equal(readContent, content) {
			t.Fatalf("content mismatch: expected %s, got %s", content, readContent)
		}
	})

	t.Run("write without expected digest", func(t *testing.T) {
		content := []byte("test content")
		digest, err := s.WriteBlob(bytes.NewReader(content), "")
		if err != nil {
			t.Fatalf("WriteBlob failed: %v", err)
		}

		expectedDigest := computeDigest(content)
		if digest != expectedDigest {
			t.Fatalf("expected digest %s, got %s", expectedDigest, digest)
		}
	})

	t.Run("digest mismatch returns error", func(t *testing.T) {
		content := []byte("test content")
		wrongDigest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"

		_, err := s.WriteBlob(bytes.NewReader(content), wrongDigest)
		if err == nil {
			t.Fatal("expected error for digest mismatch")
		}
		if !strings.Contains(err.Error(), "digest mismatch") {
			t.Fatalf("expected digest mismatch error, got: %v", err)
		}
	})

	t.Run("deduplication - writing same content twice", func(t *testing.T) {
		content := []byte("dedupe test")
		expectedDigest := computeDigest(content)

		digest1, err := s.WriteBlob(bytes.NewReader(content), expectedDigest)
		if err != nil {
			t.Fatalf("first WriteBlob failed: %v", err)
		}

		digest2, err := s.WriteBlob(bytes.NewReader(content), expectedDigest)
		if err != nil {
			t.Fatalf("second WriteBlob failed: %v", err)
		}

		if digest1 != digest2 {
			t.Fatalf("expected same digest for duplicate content")
		}

		// Verify blob exists only once
		path, _ := s.BlobPath(digest1)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("blob file doesn't exist: %v", err)
		}
		if info.Size() != int64(len(content)) {
			t.Fatalf("expected size %d, got %d", len(content), info.Size())
		}
	})
}

func TestBlobExists(t *testing.T) {
	tmpDir := t.TempDir()
	s, _ := NewStorage(tmpDir)

	content := []byte("exists test")
	digest, _ := s.WriteBlob(bytes.NewReader(content), "")

	t.Run("blob exists after write", func(t *testing.T) {
		exists, err := s.BlobExists(digest)
		if err != nil {
			t.Fatalf("BlobExists failed: %v", err)
		}
		if !exists {
			t.Fatal("expected blob to exist")
		}
	})

	t.Run("blob doesn't exist", func(t *testing.T) {
		nonExistent := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
		exists, err := s.BlobExists(nonExistent)
		if err != nil {
			t.Fatalf("BlobExists failed: %v", err)
		}
		if exists {
			t.Fatal("expected blob to not exist")
		}
	})

	t.Run("invalid digest format", func(t *testing.T) {
		_, err := s.BlobExists("invalid")
		if err == nil {
			t.Fatal("expected error for invalid digest")
		}
	})
}

func TestBlobSize(t *testing.T) {
	tmpDir := t.TempDir()
	s, _ := NewStorage(tmpDir)

	content := []byte("size test content")
	digest, _ := s.WriteBlob(bytes.NewReader(content), "")

	t.Run("returns correct size", func(t *testing.T) {
		size, err := s.BlobSize(digest)
		if err != nil {
			t.Fatalf("BlobSize failed: %v", err)
		}
		if size != int64(len(content)) {
			t.Fatalf("expected size %d, got %d", len(content), size)
		}
	})

	t.Run("error for non-existent blob", func(t *testing.T) {
		nonExistent := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
		_, err := s.BlobSize(nonExistent)
		if err == nil {
			t.Fatal("expected error for non-existent blob")
		}
	})
}

func TestDeleteBlob(t *testing.T) {
	tmpDir := t.TempDir()
	s, _ := NewStorage(tmpDir)

	content := []byte("delete test")
	digest, _ := s.WriteBlob(bytes.NewReader(content), "")

	t.Run("delete existing blob", func(t *testing.T) {
		err := s.DeleteBlob(digest)
		if err != nil {
			t.Fatalf("DeleteBlob failed: %v", err)
		}

		exists, _ := s.BlobExists(digest)
		if exists {
			t.Fatal("blob should not exist after deletion")
		}
	})

	t.Run("delete non-existent blob is no-op", func(t *testing.T) {
		nonExistent := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
		err := s.DeleteBlob(nonExistent)
		if err != nil {
			t.Fatalf("expected no error for deleting non-existent blob, got: %v", err)
		}
	})
}

func TestConcurrentWrites(t *testing.T) {
	tmpDir := t.TempDir()
	s, _ := NewStorage(tmpDir)

	const numGoroutines = 10
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			content := []byte(fmt.Sprintf("concurrent test %d", id))
			expectedDigest := computeDigest(content)

			digest, err := s.WriteBlob(bytes.NewReader(content), expectedDigest)
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: %w", id, err)
				return
			}

			if digest != expectedDigest {
				errors <- fmt.Errorf("goroutine %d: digest mismatch", id)
				return
			}

			// Verify blob exists
			exists, err := s.BlobExists(digest)
			if err != nil || !exists {
				errors <- fmt.Errorf("goroutine %d: blob doesn't exist after write", id)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent write error: %v", err)
	}
}

func TestLargeBlob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large blob test in short mode")
	}

	tmpDir := t.TempDir()
	s, _ := NewStorage(tmpDir)

	// Create a 100MB blob
	size := 100 * 1024 * 1024
	content := make([]byte, size)
	for i := range content {
		content[i] = byte(i % 256)
	}

	expectedDigest := computeDigest(content)

	t.Run("write and read large blob", func(t *testing.T) {
		digest, err := s.WriteBlob(bytes.NewReader(content), expectedDigest)
		if err != nil {
			t.Fatalf("WriteBlob failed: %v", err)
		}

		blobSize, err := s.BlobSize(digest)
		if err != nil {
			t.Fatalf("BlobSize failed: %v", err)
		}
		if blobSize != int64(size) {
			t.Fatalf("expected size %d, got %d", size, blobSize)
		}

		// Read back and verify digest
		r, err := s.ReadBlob(digest)
		if err != nil {
			t.Fatalf("ReadBlob failed: %v", err)
		}
		defer r.Close()

		hash := sha256.New()
		written, err := io.Copy(hash, r)
		if err != nil {
			t.Fatalf("reading blob failed: %v", err)
		}
		if written != int64(size) {
			t.Fatalf("expected to read %d bytes, got %d", size, written)
		}

		readDigest := "sha256:" + hex.EncodeToString(hash.Sum(nil))
		if readDigest != expectedDigest {
			t.Fatalf("digest mismatch after reading: expected %s, got %s", expectedDigest, readDigest)
		}
	})
}

func TestRepositoryPath(t *testing.T) {
	tmpDir := t.TempDir()
	s, _ := NewStorage(tmpDir)

	t.Run("returns correct repository path", func(t *testing.T) {
		repoName := "my-org/my-repo"
		path := s.RepositoryPath(repoName)

		expected := filepath.Join(tmpDir, "repositories", "my-org/my-repo")
		if path != expected {
			t.Fatalf("expected path %s, got %s", expected, path)
		}
	})
}

// Helper functions

func checkDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("directory %s doesn't exist: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %s to be a directory", path)
	}
}

func checkFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file %s doesn't exist: %v", path, err)
	}
	if info.IsDir() {
		t.Fatalf("expected %s to be a file", path)
	}
}

func computeDigest(data []byte) string {
	hash := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(hash[:])
}
