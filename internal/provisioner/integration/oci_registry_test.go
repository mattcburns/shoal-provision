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

// Integration tests for the embedded OCI registry (Phase 5 Milestone 6).
// These tests verify integration with oras and podman clients.

package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shoal/internal/provisioner/oci"
)

// TestOCIRegistry_OrasPushPull tests pushing and pulling artifacts using oras client.
func TestOCIRegistry_OrasPushPull(t *testing.T) {
	// Check if oras is available
	if _, err := exec.LookPath("oras"); err != nil {
		t.Skip("oras client not found in PATH, skipping integration test")
	}

	// Create temporary storage directory
	storageRoot := t.TempDir()
	storage, err := oci.NewStorage(storageRoot)
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}

	// Create router without authentication for testing
	router := oci.NewRouter(storage)

	// Start test server
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	// Parse server URL to get host:port
	registryURL := strings.TrimPrefix(srv.URL, "http://")

	// Create test artifact (rootfs tarball)
	artifactDir := t.TempDir()
	artifactPath := filepath.Join(artifactDir, "ubuntu-rootfs.tar.gz")

	// Create a test tarball with some content
	testContent := []byte("This is a test rootfs tarball for OCI registry integration testing")
	if err := os.WriteFile(artifactPath, testContent, 0644); err != nil {
		t.Fatalf("create test artifact: %v", err)
	}

	// Calculate expected digest
	h := sha256.New()
	h.Write(testContent)
	expectedDigest := "sha256:" + hex.EncodeToString(h.Sum(nil))

	// Repository and tag
	repo := registryURL + "/os-images/ubuntu-rootfs"
	tag := "22.04"

	// Test: Push artifact with oras
	t.Run("OrasPush", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "oras", "push",
			repo+":"+tag,
			"--artifact-type", "application/vnd.shoal.rootfs.tar.gz",
			"--plain-http",
			artifactPath,
		)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("oras push failed: %v\nOutput: %s", err, string(output))
		}

		t.Logf("oras push output: %s", string(output))

		// Verify blob exists in storage
		exists, err := storage.BlobExists(expectedDigest)
		if err != nil {
			t.Fatalf("check blob existence: %v", err)
		}
		if !exists {
			t.Fatalf("blob %s not found in storage after push", expectedDigest)
		}
	})

	// Test: Pull artifact with oras
	t.Run("OrasPull", func(t *testing.T) {
		pullDir := t.TempDir()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "oras", "pull",
			repo+":"+tag,
			"--plain-http",
			"--output", pullDir,
		)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("oras pull failed: %v\nOutput: %s", err, string(output))
		}

		t.Logf("oras pull output: %s", string(output))

		// Verify pulled content matches original
		pulledPath := filepath.Join(pullDir, "ubuntu-rootfs.tar.gz")
		pulledContent, err := os.ReadFile(pulledPath)
		if err != nil {
			t.Fatalf("read pulled artifact: %v", err)
		}

		if !bytes.Equal(testContent, pulledContent) {
			t.Fatalf("pulled content does not match original")
		}
	})
}

// TestOCIRegistry_OrasLargeFile tests pushing and pulling large sparse files (simulating WIM).
func TestOCIRegistry_OrasLargeFile(t *testing.T) {
	// Check if oras is available
	if _, err := exec.LookPath("oras"); err != nil {
		t.Skip("oras client not found in PATH, skipping integration test")
	}

	// Skip by default as large file tests take time
	if testing.Short() {
		t.Skip("skipping large file test in short mode")
	}

	// Create temporary storage directory
	storageRoot := t.TempDir()
	storage, err := oci.NewStorage(storageRoot)
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}

	router := oci.NewRouter(storage)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	registryURL := strings.TrimPrefix(srv.URL, "http://")

	// Create large sparse file (simulating 1GB WIM, but sparse so doesn't use actual disk space)
	artifactDir := t.TempDir()
	artifactPath := filepath.Join(artifactDir, "windows-large.wim")

	// Create sparse file with 1GB nominal size but only write a few blocks
	f, err := os.Create(artifactPath)
	if err != nil {
		t.Fatalf("create sparse file: %v", err)
	}

	// Write some data at the beginning
	header := []byte("MSWIM\x00\x00\x00") // Fake WIM header
	if _, err := f.Write(header); err != nil {
		f.Close()
		t.Fatalf("write header: %v", err)
	}

	// Seek to create sparse file (1GB nominal size)
	const sparseSize = 1024 * 1024 * 1024 // 1GB
	if _, err := f.Seek(sparseSize-100, 0); err != nil {
		f.Close()
		t.Fatalf("seek: %v", err)
	}

	// Write some data at the end
	footer := []byte("END_OF_WIM_FILE_MARKER_TEST_DATA")
	if _, err := f.Write(footer); err != nil {
		f.Close()
		t.Fatalf("write footer: %v", err)
	}
	f.Close()

	repo := registryURL + "/os-images/windows-wim"
	tag := "server2022"

	// Test: Push large sparse file
	t.Run("OrasPushLarge", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		cmd := exec.CommandContext(ctx, "oras", "push",
			repo+":"+tag,
			"--artifact-type", "application/vnd.shoal.windows.wim",
			"--plain-http",
			artifactPath,
		)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("oras push large file failed: %v\nOutput: %s", err, string(output))
		}

		t.Logf("oras push large file output: %s", string(output))
	})

	// Test: Pull large sparse file
	t.Run("OrasPullLarge", func(t *testing.T) {
		pullDir := t.TempDir()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		cmd := exec.CommandContext(ctx, "oras", "pull",
			repo+":"+tag,
			"--plain-http",
			"--output", pullDir,
		)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("oras pull large file failed: %v\nOutput: %s", err, string(output))
		}

		t.Logf("oras pull large file output: %s", string(output))

		// Verify pulled file exists and has correct size
		pulledPath := filepath.Join(pullDir, "windows-large.wim")
		stat, err := os.Stat(pulledPath)
		if err != nil {
			t.Fatalf("stat pulled file: %v", err)
		}

		// Check file size matches (within reasonable tolerance for sparse files)
		if stat.Size() < sparseSize-1000 {
			t.Fatalf("pulled file size mismatch: got %d, expected ~%d", stat.Size(), sparseSize)
		}

		// Verify footer content to ensure file integrity
		f, err := os.Open(pulledPath)
		if err != nil {
			t.Fatalf("open pulled file: %v", err)
		}
		defer f.Close()

		// Seek to footer position
		if _, err := f.Seek(sparseSize-100, 0); err != nil {
			t.Fatalf("seek to footer: %v", err)
		}

		footerBuf := make([]byte, len(footer))
		if _, err := io.ReadFull(f, footerBuf); err != nil {
			t.Fatalf("read footer: %v", err)
		}

		if !bytes.Equal(footer, footerBuf) {
			t.Fatalf("footer mismatch: got %q, want %q", footerBuf, footer)
		}
	})
}

// TestOCIRegistry_PodmanPushPull tests pushing and pulling container images using podman.
func TestOCIRegistry_PodmanPushPull(t *testing.T) {
	// Check if podman is available
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman client not found in PATH, skipping integration test")
	}

	// Create temporary storage directory
	storageRoot := t.TempDir()
	storage, err := oci.NewStorage(storageRoot)
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}

	router := oci.NewRouter(storage)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	registryURL := strings.TrimPrefix(srv.URL, "http://")

	// Use a small public image for testing (alpine is lightweight)
	sourceImage := "docker.io/library/alpine:latest"
	localTag := registryURL + "/tools/alpine:test"

	// Test: Pull image from Docker Hub and push to embedded registry
	t.Run("PodmanPullAndPush", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		// Pull from Docker Hub
		t.Log("Pulling alpine image from Docker Hub...")
		pullCmd := exec.CommandContext(ctx, "podman", "pull", sourceImage)
		pullOutput, err := pullCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("podman pull failed: %v\nOutput: %s", err, string(pullOutput))
		}

		// Tag for local registry
		tagCmd := exec.CommandContext(ctx, "podman", "tag", sourceImage, localTag)
		tagOutput, err := tagCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("podman tag failed: %v\nOutput: %s", err, string(tagOutput))
		}

		// Push to embedded registry
		t.Log("Pushing to embedded registry...")
		pushCmd := exec.CommandContext(ctx, "podman", "push",
			"--tls-verify=false",
			localTag,
		)
		pushOutput, err := pushCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("podman push failed: %v\nOutput: %s", err, string(pushOutput))
		}

		t.Logf("podman push output: %s", string(pushOutput))
	})

	// Test: Pull image from embedded registry
	t.Run("PodmanPullFromRegistry", func(t *testing.T) {
		// Remove local image first
		removeCmd := exec.Command("podman", "rmi", "-f", localTag)
		removeCmd.Run() // Ignore errors if image doesn't exist

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		// Pull from embedded registry
		t.Log("Pulling from embedded registry...")
		pullCmd := exec.CommandContext(ctx, "podman", "pull",
			"--tls-verify=false",
			localTag,
		)
		pullOutput, err := pullCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("podman pull from registry failed: %v\nOutput: %s", err, string(pullOutput))
		}

		t.Logf("podman pull output: %s", string(pullOutput))

		// Verify image exists locally
		inspectCmd := exec.CommandContext(ctx, "podman", "inspect", localTag)
		inspectOutput, err := inspectCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("podman inspect failed: %v\nOutput: %s", err, string(inspectOutput))
		}

		// Parse inspect output to verify it's a valid image
		var inspectData []map[string]interface{}
		if err := json.Unmarshal(inspectOutput, &inspectData); err != nil {
			t.Fatalf("parse inspect output: %v", err)
		}

		if len(inspectData) == 0 {
			t.Fatalf("no image data in inspect output")
		}

		t.Logf("Successfully pulled and inspected image from embedded registry")
	})

	// Test: Run container from pulled image
	t.Run("PodmanRunContainer", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()

		// Run a simple command in the container
		runCmd := exec.CommandContext(ctx, "podman", "run", "--rm", localTag, "echo", "test-success")
		runOutput, err := runCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("podman run failed: %v\nOutput: %s", err, string(runOutput))
		}

		output := strings.TrimSpace(string(runOutput))
		if !strings.Contains(output, "test-success") {
			t.Fatalf("unexpected container output: %s", output)
		}

		t.Logf("Container ran successfully: %s", output)
	})

	// Cleanup: Remove test images
	t.Cleanup(func() {
		exec.Command("podman", "rmi", "-f", localTag).Run()
		exec.Command("podman", "rmi", "-f", sourceImage).Run()
	})
}

// TestOCIRegistry_ConcurrentUploads tests concurrent uploads to different repositories.
func TestOCIRegistry_ConcurrentUploads(t *testing.T) {
	// Check if oras is available
	if _, err := exec.LookPath("oras"); err != nil {
		t.Skip("oras client not found in PATH, skipping integration test")
	}

	// Create temporary storage directory
	storageRoot := t.TempDir()
	storage, err := oci.NewStorage(storageRoot)
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}

	router := oci.NewRouter(storage)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	registryURL := strings.TrimPrefix(srv.URL, "http://")

	// Create multiple test artifacts
	const numConcurrent = 8
	artifacts := make([]string, numConcurrent)
	artifactDir := t.TempDir()

	for i := 0; i < numConcurrent; i++ {
		artifacts[i] = filepath.Join(artifactDir, fmt.Sprintf("artifact-%d.tar.gz", i))
		content := []byte(fmt.Sprintf("Test artifact content for concurrent upload test #%d", i))
		if err := os.WriteFile(artifacts[i], content, 0644); err != nil {
			t.Fatalf("create artifact %d: %v", i, err)
		}
	}

	// Test: Concurrent uploads
	errCh := make(chan error, numConcurrent)
	doneCh := make(chan bool, numConcurrent)

	for i := 0; i < numConcurrent; i++ {
		go func(idx int) {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()

			repo := fmt.Sprintf("%s/concurrent/repo-%d", registryURL, idx)
			cmd := exec.CommandContext(ctx, "oras", "push",
				repo+":test",
				"--artifact-type", "application/vnd.test.concurrent",
				"--plain-http",
				artifacts[idx],
			)

			output, err := cmd.CombinedOutput()
			if err != nil {
				errCh <- fmt.Errorf("upload %d failed: %w\nOutput: %s", idx, err, string(output))
			} else {
				doneCh <- true
			}
		}(i)
	}

	// Wait for all uploads to complete
	successCount := 0
	for i := 0; i < numConcurrent; i++ {
		select {
		case err := <-errCh:
			t.Errorf("concurrent upload error: %v", err)
		case <-doneCh:
			successCount++
		case <-time.After(2 * time.Minute):
			t.Fatalf("timeout waiting for concurrent uploads")
		}
	}

	if successCount != numConcurrent {
		t.Fatalf("expected %d successful uploads, got %d", numConcurrent, successCount)
	}

	t.Logf("Successfully completed %d concurrent uploads", successCount)
}
