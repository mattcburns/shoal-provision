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

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shoal/internal/provisioner/maintenance/bootloader"
	"shoal/internal/provisioner/maintenance/configdrive"
	"shoal/internal/provisioner/maintenance/image"
	"shoal/internal/provisioner/maintenance/partition"
)

// TestLinuxWorkflowIntegration validates the complete Linux provisioning workflow
// by orchestrating all planning steps and verifying their outputs.
func TestLinuxWorkflowIntegration(t *testing.T) {
	// Define a typical minimal UEFI layout for Linux
	layout := []map[string]interface{}{
		{
			"size":      "512M",
			"type_guid": "ef00",
			"format":    "vfat",
			"label":     "ESP",
		},
		{
			"size":      "100%",
			"type_guid": "8300",
			"format":    "ext4",
			"label":     "rootfs",
		},
		{
			"size":      "16M",
			"type_guid": "8300",
			"format":    "vfat",
			"label":     "cidata",
		},
	}

	layoutJSON, err := json.Marshal(layout)
	if err != nil {
		t.Fatalf("marshal layout: %v", err)
	}

	targetDisk := "/dev/sda"
	ociURL := "localhost:8080/os-images/ubuntu:22.04"
	rootPath := "/mnt/new-root"
	espMount := "/mnt/efi"
	cidataMount := "/mnt/cidata"

	t.Run("Partition Planning", func(t *testing.T) {
		cmds, err := partition.Plan(targetDisk, layoutJSON)
		if err != nil {
			t.Fatalf("partition.Plan failed: %v", err)
		}

		if len(cmds) == 0 {
			t.Fatal("expected partition commands, got none")
		}

		// Verify that partition commands contain expected operations
		shellOutput := make([]string, len(cmds))
		for i, cmd := range cmds {
			shellOutput[i] = cmd.Shell()
		}

		combined := strings.Join(shellOutput, "\n")

		// Should contain sgdisk operations
		if !strings.Contains(combined, "sgdisk") {
			t.Error("expected sgdisk command in partition plan")
		}

		// Should reference the target disk
		if !strings.Contains(combined, targetDisk) {
			t.Errorf("expected disk %s in partition plan", targetDisk)
		}

		// Should create filesystems
		if !strings.Contains(combined, "mkfs") {
			t.Error("expected mkfs commands in partition plan")
		}

		t.Logf("Partition plan generated %d commands", len(cmds))
	})

	t.Run("Image Planning", func(t *testing.T) {
		cmds, err := image.Plan(image.Options{
			OCIURL:   ociURL,
			RootPath: rootPath,
		})
		if err != nil {
			t.Fatalf("image.Plan failed: %v", err)
		}

		if len(cmds) == 0 {
			t.Fatal("expected image commands, got none")
		}

		shellOutput := make([]string, len(cmds))
		for i, cmd := range cmds {
			shellOutput[i] = cmd.Shell()
		}

		combined := strings.Join(shellOutput, "\n")

		// Should create root directory
		if !strings.Contains(combined, rootPath) {
			t.Errorf("expected root path %s in image plan", rootPath)
		}

		// Should reference OCI URL
		if !strings.Contains(combined, ociURL) {
			t.Errorf("expected OCI URL %s in image plan", ociURL)
		}

		// Should use oras for pulling
		if !strings.Contains(combined, "oras") && !strings.Contains(combined, "podman") {
			t.Error("expected oras or podman in image plan")
		}

		// Should use tar for extraction
		if !strings.Contains(combined, "tar") {
			t.Error("expected tar in image plan")
		}

		t.Logf("Image plan generated %d commands", len(cmds))
	})

	t.Run("Bootloader Planning", func(t *testing.T) {
		espDevice := targetDisk + "1"
		rootDevice := targetDisk + "2"

		cmds, err := bootloader.Plan(bootloader.Options{
			RootPath:     rootPath,
			ESPMountPath: espMount,
			ESPDevice:    espDevice,
			RootDevice:   rootDevice,
			RootFSType:   "ext4",
			BootloaderID: "Shoal",
			GrubTarget:   "x86_64-efi",
		})
		if err != nil {
			t.Fatalf("bootloader.Plan failed: %v", err)
		}

		if len(cmds) == 0 {
			t.Fatal("expected bootloader commands, got none")
		}

		shellOutput := make([]string, len(cmds))
		for i, cmd := range cmds {
			shellOutput[i] = cmd.Shell()
		}

		combined := strings.Join(shellOutput, "\n")

		// Should mount ESP
		if !strings.Contains(combined, espMount) {
			t.Errorf("expected ESP mount %s in bootloader plan", espMount)
		}

		// Should generate fstab
		if !strings.Contains(combined, "fstab") {
			t.Error("expected fstab generation in bootloader plan")
		}

		// Should install GRUB
		if !strings.Contains(combined, "grub") {
			t.Error("expected grub installation in bootloader plan")
		}

		t.Logf("Bootloader plan generated %d commands", len(cmds))
	})

	t.Run("Config Drive Planning", func(t *testing.T) {
		// Create temporary files for user-data and meta-data
		tmpDir := t.TempDir()
		userDataPath := filepath.Join(tmpDir, "user-data")
		metaDataPath := filepath.Join(tmpDir, "meta-data")

		userData := "#cloud-config\nhostname: test-host\n"
		if err := os.WriteFile(userDataPath, []byte(userData), 0644); err != nil {
			t.Fatalf("write user-data: %v", err)
		}

		cmds, err := configdrive.Plan(configdrive.Options{
			MountPath:    cidataMount,
			Device:       targetDisk + "3",
			UserDataPath: userDataPath,
			MetaDataPath: metaDataPath,
			InstanceID:   "test-instance-123",
			Hostname:     "test-host",
		})
		if err != nil {
			t.Fatalf("configdrive.Plan failed: %v", err)
		}

		if len(cmds) == 0 {
			t.Fatal("expected config drive commands, got none")
		}

		shellOutput := make([]string, len(cmds))
		for i, cmd := range cmds {
			shellOutput[i] = cmd.Shell()
		}

		combined := strings.Join(shellOutput, "\n")

		// Should mount CIDATA partition
		if !strings.Contains(combined, cidataMount) {
			t.Errorf("expected cidata mount %s in config drive plan", cidataMount)
		}

		// Should reference user-data
		if !strings.Contains(combined, "user-data") {
			t.Error("expected user-data in config drive plan")
		}

		t.Logf("Config drive plan generated %d commands", len(cmds))
	})
}

// TestLinuxWorkflowIdempotency validates that workflow steps can be safely re-run
// when the system is already in the desired state.
func TestLinuxWorkflowIdempotency(t *testing.T) {
	layout := []map[string]interface{}{
		{
			"size":      "512M",
			"type_guid": "ef00",
			"format":    "vfat",
			"label":     "ESP",
		},
		{
			"size":      "100%",
			"type_guid": "8300",
			"format":    "ext4",
			"label":     "rootfs",
		},
	}

	layoutJSON, err := json.Marshal(layout)
	if err != nil {
		t.Fatalf("marshal layout: %v", err)
	}

	t.Run("Partition Idempotency", func(t *testing.T) {
		// Running the same partition plan twice should produce
		// the same commands (the wrapper handles idempotency)
		cmds1, err := partition.Plan("/dev/sda", layoutJSON)
		if err != nil {
			t.Fatalf("first partition.Plan failed: %v", err)
		}

		cmds2, err := partition.Plan("/dev/sda", layoutJSON)
		if err != nil {
			t.Fatalf("second partition.Plan failed: %v", err)
		}

		if len(cmds1) != len(cmds2) {
			t.Errorf("command count mismatch: %d vs %d", len(cmds1), len(cmds2))
		}

		// Commands should be deterministic
		for i := range cmds1 {
			if i >= len(cmds2) {
				break
			}
			shell1 := cmds1[i].Shell()
			shell2 := cmds2[i].Shell()
			if shell1 != shell2 {
				t.Errorf("command %d differs:\n  first:  %s\n  second: %s", i, shell1, shell2)
			}
		}
	})

	t.Run("Image Idempotency", func(t *testing.T) {
		opts := image.Options{
			OCIURL:   "localhost:8080/os-images/ubuntu:22.04",
			RootPath: "/mnt/new-root",
		}

		cmds1, err := image.Plan(opts)
		if err != nil {
			t.Fatalf("first image.Plan failed: %v", err)
		}

		cmds2, err := image.Plan(opts)
		if err != nil {
			t.Fatalf("second image.Plan failed: %v", err)
		}

		if len(cmds1) != len(cmds2) {
			t.Errorf("command count mismatch: %d vs %d", len(cmds1), len(cmds2))
		}
	})

	t.Run("Bootloader Idempotency", func(t *testing.T) {
		opts := bootloader.Options{
			RootPath:     "/mnt/new-root",
			ESPMountPath: "/mnt/efi",
			ESPDevice:    "/dev/sda1",
			RootDevice:   "/dev/sda2",
			RootFSType:   "ext4",
			BootloaderID: "Shoal",
			GrubTarget:   "x86_64-efi",
		}

		cmds1, err := bootloader.Plan(opts)
		if err != nil {
			t.Fatalf("first bootloader.Plan failed: %v", err)
		}

		cmds2, err := bootloader.Plan(opts)
		if err != nil {
			t.Fatalf("second bootloader.Plan failed: %v", err)
		}

		if len(cmds1) != len(cmds2) {
			t.Errorf("command count mismatch: %d vs %d", len(cmds1), len(cmds2))
		}
	})
}

// TestLinuxWorkflowErrorConditions tests various failure scenarios
func TestLinuxWorkflowErrorConditions(t *testing.T) {
	t.Run("Invalid Partition Layout", func(t *testing.T) {
		invalidJSON := []byte(`{"invalid": "data"}`)
		_, err := partition.Plan("/dev/sda", invalidJSON)
		if err == nil {
			t.Error("expected error for invalid layout, got nil")
		}
	})

	t.Run("Missing Required Fields in Image Options", func(t *testing.T) {
		// Empty OCI URL should fail
		_, err := image.Plan(image.Options{
			OCIURL:   "",
			RootPath: "/mnt/new-root",
		})
		if err == nil {
			t.Error("expected error for empty OCI URL, got nil")
		}
	})

	t.Run("Missing Required Fields in Bootloader Options", func(t *testing.T) {
		// Missing ESPDevice should fail
		_, err := bootloader.Plan(bootloader.Options{
			RootPath:     "/mnt/new-root",
			ESPMountPath: "/mnt/efi",
			ESPDevice:    "",
			RootDevice:   "/dev/sda2",
		})
		if err == nil {
			t.Error("expected error for missing ESP device, got nil")
		}
	})
}

// TestLinuxWorkflowOutputFormats tests that all planners support both shell and JSON output
func TestLinuxWorkflowOutputFormats(t *testing.T) {
	layout := []map[string]interface{}{
		{
			"size":      "512M",
			"type_guid": "ef00",
			"format":    "vfat",
			"label":     "ESP",
		},
	}

	layoutJSON, err := json.Marshal(layout)
	if err != nil {
		t.Fatalf("marshal layout: %v", err)
	}

	t.Run("Partition Output Formats", func(t *testing.T) {
		cmds, err := partition.Plan("/dev/sda", layoutJSON)
		if err != nil {
			t.Fatalf("partition.Plan failed: %v", err)
		}

		for i, cmd := range cmds {
			// Test Shell format
			shell := cmd.Shell()
			if shell == "" {
				t.Errorf("command %d: Shell() returned empty string", i)
			}

			// Test JSON marshaling
			data, err := json.Marshal(cmd)
			if err != nil {
				t.Errorf("command %d: JSON marshal failed: %v", i, err)
			}
			if len(data) == 0 {
				t.Errorf("command %d: JSON marshal returned empty", i)
			}

			// Verify JSON can be unmarshaled
			var buf bytes.Buffer
			if err := json.Indent(&buf, data, "", "  "); err != nil {
				t.Errorf("command %d: JSON indent failed: %v", i, err)
			}
		}
	})
}

// BenchmarkLinuxWorkflowPlanning measures the performance of all planning steps
func BenchmarkLinuxWorkflowPlanning(b *testing.B) {
	layout := []map[string]interface{}{
		{"size": "512M", "type_guid": "ef00", "format": "vfat", "label": "ESP"},
		{"size": "100%", "type_guid": "8300", "format": "ext4", "label": "rootfs"},
	}
	layoutJSON, _ := json.Marshal(layout)

	b.Run("Partition", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := partition.Plan("/dev/sda", layoutJSON)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Image", func(b *testing.B) {
		opts := image.Options{
			OCIURL:   "localhost:8080/os-images/ubuntu:22.04",
			RootPath: "/mnt/new-root",
		}
		for i := 0; i < b.N; i++ {
			_, err := image.Plan(opts)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Bootloader", func(b *testing.B) {
		opts := bootloader.Options{
			RootPath:     "/mnt/new-root",
			ESPMountPath: "/mnt/efi",
			ESPDevice:    "/dev/sda1",
			RootDevice:   "/dev/sda2",
			RootFSType:   "ext4",
			BootloaderID: "Shoal",
			GrubTarget:   "x86_64-efi",
		}
		for i := 0; i < b.N; i++ {
			_, err := bootloader.Plan(opts)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// exampleRecipeEnv demonstrates the expected recipe.env format
func exampleRecipeEnv() string {
	return fmt.Sprintf(`TASK_TARGET=install-linux.target
TARGET_DISK=/dev/nvme0n1
OCI_URL=controller.internal:8080/os-images/ubuntu-rootfs:22.04
SERIAL_NUMBER=XF-12345ABC
WEBHOOK_URL=http://controller.internal:8080
WEBHOOK_SECRET=redacted
PARTITION_APPLY=1
IMAGE_APPLY=1
BOOTLOADER_APPLY=1
`)
}
