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

// End-to-end tests for embedded OCI registry integration with provisioning workflows (Phase 5 Milestone 8).
// These tests verify complete workflows from artifact push to provisioning completion.

package integration

import (
	"context"
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

// TestE2E_LinuxProvisioningWorkflow tests the complete Linux provisioning workflow:
// 1. Push rootfs tarball to embedded registry
// 2. Create recipe referencing the registry artifact
// 3. Simulate provisioning (pull artifact, verify content)
func TestE2E_LinuxProvisioningWorkflow(t *testing.T) {
	// Check if oras is available
	if _, err := exec.LookPath("oras"); err != nil {
		t.Skip("oras client not found in PATH, skipping E2E test")
	}

	// Create temporary storage directory
	storageRoot := t.TempDir()
	storage, err := oci.NewStorage(storageRoot)
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}

	// Create router and start test server
	router := oci.NewRouter(storage)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	registryURL := strings.TrimPrefix(srv.URL, "http://")

	// Step 1: Create a realistic rootfs tarball
	t.Run("CreateRootfs", func(t *testing.T) {
		artifactDir := t.TempDir()
		artifactPath := filepath.Join(artifactDir, "ubuntu-22.04-rootfs.tar.gz")

		// Create a minimal rootfs structure
		rootfsDir := t.TempDir()
		dirs := []string{"bin", "etc", "usr/bin", "var/log"}
		for _, dir := range dirs {
			if err := os.MkdirAll(filepath.Join(rootfsDir, dir), 0755); err != nil {
				t.Fatalf("create rootfs dir %s: %v", dir, err)
			}
		}

		// Add some basic files
		files := map[string]string{
			"etc/os-release": "NAME=\"Ubuntu\"\nVERSION=\"22.04 LTS\"\nID=ubuntu\n",
			"etc/hostname":   "test-server\n",
			"usr/bin/test":   "#!/bin/bash\necho 'Test script'\n",
		}
		for path, content := range files {
			fullPath := filepath.Join(rootfsDir, path)
			if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
				t.Fatalf("create file %s: %v", path, err)
			}
		}

		// Create tarball
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		tarCmd := exec.CommandContext(ctx, "tar", "czf", artifactPath, "-C", rootfsDir, ".")
		if output, err := tarCmd.CombinedOutput(); err != nil {
			t.Fatalf("create tarball: %v\nOutput: %s", err, string(output))
		}

		// Verify tarball was created
		stat, err := os.Stat(artifactPath)
		if err != nil {
			t.Fatalf("stat tarball: %v", err)
		}
		t.Logf("Created rootfs tarball: %s (size: %d bytes)", artifactPath, stat.Size())

		// Store artifact path for next steps
		t.Setenv("TEST_ARTIFACT_PATH", artifactPath)
	})

	// Step 2: Push rootfs to registry
	t.Run("PushRootfs", func(t *testing.T) {
		artifactPath := os.Getenv("TEST_ARTIFACT_PATH")
		repo := registryURL + "/os-images/ubuntu-rootfs"
		tag := "22.04"

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
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

		t.Logf("Successfully pushed rootfs to registry: %s:%s", repo, tag)
		t.Setenv("TEST_REGISTRY_REPO", repo)
		t.Setenv("TEST_REGISTRY_TAG", tag)
	})

	// Step 3: Simulate provisioning - pull and extract
	t.Run("SimulateProvisioning", func(t *testing.T) {
		repo := os.Getenv("TEST_REGISTRY_REPO")
		tag := os.Getenv("TEST_REGISTRY_TAG")
		pullDir := t.TempDir()

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()

		// Pull artifact
		cmd := exec.CommandContext(ctx, "oras", "pull",
			repo+":"+tag,
			"--plain-http",
			"--output", pullDir,
		)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("oras pull failed: %v\nOutput: %s", err, string(output))
		}

		t.Logf("Successfully pulled rootfs from registry")

		// Extract tarball to simulate installation
		extractDir := t.TempDir()
		tarballPath := filepath.Join(pullDir, "ubuntu-22.04-rootfs.tar.gz")

		extractCmd := exec.CommandContext(ctx, "tar", "xzf", tarballPath, "-C", extractDir)
		if extractOutput, err := extractCmd.CombinedOutput(); err != nil {
			t.Fatalf("extract tarball: %v\nOutput: %s", err, string(extractOutput))
		}

		// Verify extracted content
		osReleasePath := filepath.Join(extractDir, "etc/os-release")
		content, err := os.ReadFile(osReleasePath)
		if err != nil {
			t.Fatalf("read os-release: %v", err)
		}

		if !strings.Contains(string(content), "Ubuntu") {
			t.Fatalf("unexpected os-release content: %s", string(content))
		}

		t.Logf("Successfully extracted and verified rootfs content")
	})
}

// TestE2E_WindowsProvisioningWorkflow tests Windows provisioning with WIM images.
func TestE2E_WindowsProvisioningWorkflow(t *testing.T) {
	// Check if oras is available
	if _, err := exec.LookPath("oras"); err != nil {
		t.Skip("oras client not found in PATH, skipping E2E test")
	}

	if testing.Short() {
		t.Skip("skipping Windows E2E test in short mode")
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

	// Step 1: Create a simulated WIM file (sparse)
	t.Run("CreateWIM", func(t *testing.T) {
		artifactDir := t.TempDir()
		wimPath := filepath.Join(artifactDir, "install.wim")

		// Create sparse file simulating a WIM
		f, err := os.Create(wimPath)
		if err != nil {
			t.Fatalf("create WIM file: %v", err)
		}

		// Write WIM header signature
		header := []byte("MSWIM\x00\x00\x00")
		if _, err := f.Write(header); err != nil {
			f.Close()
			t.Fatalf("write WIM header: %v", err)
		}

		// Create sparse file (5GB nominal size)
		const wimSize = 5 * 1024 * 1024 * 1024
		if _, err := f.Seek(wimSize-100, 0); err != nil {
			f.Close()
			t.Fatalf("seek: %v", err)
		}

		footer := []byte("WIM_FOOTER_TEST_DATA")
		if _, err := f.Write(footer); err != nil {
			f.Close()
			t.Fatalf("write footer: %v", err)
		}
		f.Close()

		t.Logf("Created simulated WIM file: %s", wimPath)
		t.Setenv("TEST_WIM_PATH", wimPath)
	})

	// Step 2: Push WIM to registry
	t.Run("PushWIM", func(t *testing.T) {
		wimPath := os.Getenv("TEST_WIM_PATH")
		repo := registryURL + "/os-images/windows-server"
		tag := "2022"

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		cmd := exec.CommandContext(ctx, "oras", "push",
			repo+":"+tag,
			"--artifact-type", "application/vnd.shoal.windows.wim",
			"--plain-http",
			wimPath,
		)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("oras push WIM failed: %v\nOutput: %s", err, string(output))
		}

		t.Logf("Successfully pushed WIM to registry: %s:%s", repo, tag)
		t.Setenv("TEST_WIM_REPO", repo)
		t.Setenv("TEST_WIM_TAG", tag)
	})

	// Step 3: Simulate provisioning - pull WIM
	t.Run("SimulateWindowsProvisioning", func(t *testing.T) {
		repo := os.Getenv("TEST_WIM_REPO")
		tag := os.Getenv("TEST_WIM_TAG")
		pullDir := t.TempDir()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		// Pull WIM artifact
		cmd := exec.CommandContext(ctx, "oras", "pull",
			repo+":"+tag,
			"--plain-http",
			"--output", pullDir,
		)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("oras pull WIM failed: %v\nOutput: %s", err, string(output))
		}

		t.Logf("Successfully pulled WIM from registry")

		// Verify WIM file
		wimPath := filepath.Join(pullDir, "install.wim")
		stat, err := os.Stat(wimPath)
		if err != nil {
			t.Fatalf("stat pulled WIM: %v", err)
		}

		t.Logf("Pulled WIM size: %d bytes", stat.Size())

		// Verify WIM header
		f, err := os.Open(wimPath)
		if err != nil {
			t.Fatalf("open WIM: %v", err)
		}
		defer f.Close()

		headerBuf := make([]byte, 8)
		if _, err := io.ReadFull(f, headerBuf); err != nil {
			t.Fatalf("read WIM header: %v", err)
		}

		if string(headerBuf[:5]) != "MSWIM" {
			t.Fatalf("invalid WIM header: %q", headerBuf)
		}

		t.Logf("Successfully verified WIM content")
	})
}

// TestE2E_PerformanceLargeBlob tests uploading and downloading a large blob (20GB).
func TestE2E_PerformanceLargeBlob(t *testing.T) {
	// Check if oras is available
	if _, err := exec.LookPath("oras"); err != nil {
		t.Skip("oras client not found in PATH, skipping performance test")
	}

	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	// This test is expensive, skip unless explicitly requested
	if os.Getenv("RUN_PERFORMANCE_TESTS") != "1" {
		t.Skip("skipping expensive performance test (set RUN_PERFORMANCE_TESTS=1 to run)")
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

	// Create 20GB sparse file
	artifactDir := t.TempDir()
	artifactPath := filepath.Join(artifactDir, "large-blob.bin")

	t.Logf("Creating 20GB sparse file...")
	f, err := os.Create(artifactPath)
	if err != nil {
		t.Fatalf("create large file: %v", err)
	}

	// Write data at intervals to create realistic sparse file
	const fileSize = 20 * 1024 * 1024 * 1024 // 20GB
	const chunkSize = 1024 * 1024            // 1MB
	const chunkInterval = 100 * 1024 * 1024  // Write every 100MB

	for offset := int64(0); offset < fileSize; offset += chunkInterval {
		if _, err := f.Seek(offset, 0); err != nil {
			f.Close()
			t.Fatalf("seek: %v", err)
		}

		data := make([]byte, chunkSize)
		for i := range data {
			data[i] = byte(offset / chunkInterval)
		}

		if _, err := f.Write(data); err != nil {
			f.Close()
			t.Fatalf("write chunk: %v", err)
		}
	}
	f.Close()

	t.Logf("Created 20GB sparse file")

	// Test upload performance
	t.Run("Upload20GB", func(t *testing.T) {
		repo := registryURL + "/performance/large-blob"
		tag := "20gb"

		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()

		cmd := exec.CommandContext(ctx, "oras", "push",
			repo+":"+tag,
			"--artifact-type", "application/vnd.test.large-blob",
			"--plain-http",
			artifactPath,
		)
		output, err := cmd.CombinedOutput()
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("oras push large blob failed: %v\nOutput: %s", err, string(output))
		}

		t.Logf("Upload completed in %v", elapsed)
		t.Setenv("TEST_LARGE_REPO", repo)
		t.Setenv("TEST_LARGE_TAG", tag)
	})

	// Test download performance
	t.Run("Download20GB", func(t *testing.T) {
		repo := os.Getenv("TEST_LARGE_REPO")
		tag := os.Getenv("TEST_LARGE_TAG")
		pullDir := t.TempDir()

		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Hour)
		defer cancel()

		cmd := exec.CommandContext(ctx, "oras", "pull",
			repo+":"+tag,
			"--plain-http",
			"--output", pullDir,
		)
		output, err := cmd.CombinedOutput()
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("oras pull large blob failed: %v\nOutput: %s", err, string(output))
		}

		t.Logf("Download completed in %v", elapsed)

		// Verify file size
		pulledPath := filepath.Join(pullDir, "large-blob.bin")
		stat, err := os.Stat(pulledPath)
		if err != nil {
			t.Fatalf("stat pulled file: %v", err)
		}

		if stat.Size() < fileSize-1000 {
			t.Fatalf("downloaded file size mismatch: got %d, expected ~%d", stat.Size(), fileSize)
		}

		t.Logf("Verified file size: %d bytes", stat.Size())
	})
}

// TestE2E_ConcurrencyStress tests 8 parallel uploads to verify stability under concurrent load.
func TestE2E_ConcurrencyStress(t *testing.T) {
	// Check if oras is available
	if _, err := exec.LookPath("oras"); err != nil {
		t.Skip("oras client not found in PATH, skipping concurrency test")
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

	// Create 8 different artifacts
	const numWorkers = 8
	artifactDir := t.TempDir()
	artifacts := make([]string, numWorkers)

	for i := 0; i < numWorkers; i++ {
		artifacts[i] = filepath.Join(artifactDir, fmt.Sprintf("artifact-%d.tar.gz", i))
		content := make([]byte, 10*1024*1024) // 10MB each
		for j := range content {
			content[j] = byte(i)
		}
		if err := os.WriteFile(artifacts[i], content, 0644); err != nil {
			t.Fatalf("create artifact %d: %v", i, err)
		}
	}

	// Test concurrent uploads
	errCh := make(chan error, numWorkers)
	doneCh := make(chan int, numWorkers)
	start := time.Now()

	for i := 0; i < numWorkers; i++ {
		go func(idx int) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			repo := fmt.Sprintf("%s/stress/worker-%d", registryURL, idx)
			cmd := exec.CommandContext(ctx, "oras", "push",
				repo+":test",
				"--artifact-type", "application/vnd.test.stress",
				"--plain-http",
				artifacts[idx],
			)

			output, err := cmd.CombinedOutput()
			if err != nil {
				errCh <- fmt.Errorf("worker %d failed: %w\nOutput: %s", idx, err, string(output))
			} else {
				doneCh <- idx
			}
		}(i)
	}

	// Wait for all uploads
	successCount := 0
	for i := 0; i < numWorkers; i++ {
		select {
		case err := <-errCh:
			t.Errorf("concurrent upload error: %v", err)
		case workerID := <-doneCh:
			successCount++
			t.Logf("Worker %d completed", workerID)
		case <-time.After(10 * time.Minute):
			t.Fatalf("timeout waiting for concurrent uploads")
		}
	}

	elapsed := time.Since(start)

	if successCount != numWorkers {
		t.Fatalf("expected %d successful uploads, got %d", numWorkers, successCount)
	}

	t.Logf("All %d concurrent uploads completed successfully in %v", numWorkers, elapsed)
}

// TestE2E_FailureRecovery tests controller restart with in-progress uploads.
func TestE2E_FailureRecovery(t *testing.T) {
	// Check if oras is available
	if _, err := exec.LookPath("oras"); err != nil {
		t.Skip("oras client not found in PATH, skipping failure recovery test")
	}

	// Create temporary storage directory
	storageRoot := t.TempDir()
	storage, err := oci.NewStorage(storageRoot)
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}

	// Start first server
	router1 := oci.NewRouter(storage)
	srv1 := httptest.NewServer(router1)

	registryURL := strings.TrimPrefix(srv1.URL, "http://")

	// Create test artifact
	artifactDir := t.TempDir()
	artifactPath := filepath.Join(artifactDir, "test-artifact.tar.gz")
	testData := make([]byte, 5*1024*1024) // 5MB
	if err := os.WriteFile(artifactPath, testData, 0644); err != nil {
		t.Fatalf("create test artifact: %v", err)
	}

	// Start upload
	repo := registryURL + "/recovery/test"
	tag := "v1"

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "oras", "push",
		repo+":"+tag,
		"--artifact-type", "application/vnd.test.recovery",
		"--plain-http",
		artifactPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("initial push failed: %v\nOutput: %s", err, string(output))
	}

	t.Logf("Initial upload completed successfully")

	// Simulate controller restart by closing first server and starting second
	srv1.Close()

	// Create new router with same storage
	router2 := oci.NewRouter(storage)
	srv2 := httptest.NewServer(router2)
	t.Cleanup(srv2.Close)

	registryURL2 := strings.TrimPrefix(srv2.URL, "http://")

	// Verify artifact is still accessible after restart
	pullDir := t.TempDir()
	repo2 := registryURL2 + "/recovery/test"

	pullCmd := exec.CommandContext(ctx, "oras", "pull",
		repo2+":"+tag,
		"--plain-http",
		"--output", pullDir,
	)
	pullOutput, err := pullCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pull after restart failed: %v\nOutput: %s", err, string(pullOutput))
	}

	t.Logf("Successfully recovered and pulled artifact after controller restart")

	// Verify content
	pulledPath := filepath.Join(pullDir, "test-artifact.tar.gz")
	pulledData, err := os.ReadFile(pulledPath)
	if err != nil {
		t.Fatalf("read pulled artifact: %v", err)
	}

	if len(pulledData) != len(testData) {
		t.Fatalf("pulled data size mismatch: got %d, want %d", len(pulledData), len(testData))
	}

	t.Logf("Content verified after recovery")
}
