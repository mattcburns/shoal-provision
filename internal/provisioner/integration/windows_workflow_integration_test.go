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

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"shoal/internal/provisioner/dispatcher"
	"shoal/internal/provisioner/schema"
)

func TestWindowsWorkflow_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Windows workflow E2E test in short mode")
	}
	if runtime.GOOS != "linux" {
		t.Skip("Windows workflow integrations require Linux host")
	}

	ctx := context.Background()
	repoRoot := repoRootDir(t)
	tempDir := t.TempDir()
	taskDir := filepath.Join(tempDir, "task")
	envDir := filepath.Join(tempDir, "env")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatalf("mkdir env dir: %v", err)
	}

	writeFile(t, filepath.Join(taskDir, "recipe.schema.json"), string(schema.Recipe()), 0o644)

	unattendXML := `<?xml version="1.0" encoding="utf-8"?>
<unattend xmlns="urn:schemas-microsoft-com:unattend">
  <settings pass="specialize">
    <component name="Microsoft-Windows-Shell-Setup">
      <ComputerName>SHOAL-E2E</ComputerName>
    </component>
  </settings>
</unattend>`

	recipe := map[string]any{
		"schema_version": "1.0",
		"task_target":    "install-windows.target",
		"target_disk":    "/dev/sda",
		"oci_url":        "controller.internal:8080/os-images/windows-wim:22H2",
		"wim_index":      1,
		"partition_layout": []map[string]any{
			{
				"size":      "512M",
				"type_guid": "ef00",
				"format":    "vfat",
				"label":     "EFI",
			},
			{
				"size":      "16M",
				"type_guid": "0c01",
				"format":    "raw",
				"label":     "MSR",
			},
			{
				"size":      "100%",
				"type_guid": "0700",
				"format":    "ntfs",
				"label":     "Windows",
			},
		},
		"unattend_xml": map[string]string{
			"content": unattendXML,
		},
	}
	recipeBytes, err := json.MarshalIndent(recipe, "", "  ")
	if err != nil {
		t.Fatalf("marshal recipe: %v", err)
	}
	writeFile(t, filepath.Join(taskDir, "recipe.json"), string(recipeBytes), 0o644)

	stub := &commandRecorder{}
	cfg := dispatcher.Config{
		TaskMount:      taskDir,
		EnvDir:         envDir,
		SkipRootCheck:  true,
		SerialOverride: "WIN-SER123",
		Exec:           stub.run,
		Version:        "integration-test",
	}

	if err := dispatcher.Run(ctx, cfg); err != nil {
		t.Fatalf("dispatcher run failed: %v", err)
	}
	if !stub.called("systemctl", "start", "install-windows.target") {
		t.Fatalf("expected dispatcher to invoke systemctl start install-windows.target, calls=%v", stub.calls)
	}

	recipeEnvPath := filepath.Join(envDir, "recipe.env")
	recipeEnvContent := readFile(t, recipeEnvPath)
	for _, needle := range []string{
		"TASK_TARGET=install-windows.target",
		"TARGET_DISK=/dev/sda",
		"OCI_URL=controller.internal:8080/os-images/windows-wim:22H2",
		"WIM_INDEX=1",
		"SERIAL_NUMBER=WIN-SER123",
	} {
		if !strings.Contains(recipeEnvContent, needle) {
			t.Fatalf("env file missing %q; content=%s", needle, recipeEnvContent)
		}
	}

	partitionPlan := makePlanShim(t, tempDir, repoRoot, "partition-plan")
	imageWindowsPlan := makePlanShim(t, tempDir, repoRoot, "image-windows-plan")
	bootloaderWindowsPlan := makePlanShim(t, tempDir, repoRoot, "bootloader-windows-plan")

	layoutPath := filepath.Join(envDir, "layout.json")
	if _, err := os.Stat(layoutPath); err != nil {
		t.Fatalf("layout.json missing: %v", err)
	}
	unattendPath := filepath.Join(envDir, "unattend.xml")
	if _, err := os.Stat(unattendPath); err != nil {
		t.Fatalf("unattend.xml missing: %v", err)
	}

	// Verify partition plan includes Windows layout
	partitionOutput := runWrapper(t, ctx, repoRoot, "partition-wrapper.sh", map[string]string{
		"TARGET_DISK":        "/dev/sda",
		"LAYOUT_JSON_PATH":   layoutPath,
		"PARTITION_PLAN_BIN": partitionPlan,
		"PARTITION_APPLY":    "0",
	})
	assertContains(t, partitionOutput, "sgdisk", "partition plan should include sgdisk command")
	assertContains(t, partitionOutput, "mkfs.ntfs", "partition plan should include mkfs.ntfs for Windows")

	// Verify Windows image plan
	imageOutput := runWrapper(t, ctx, repoRoot, "image-windows-wrapper.sh", map[string]string{
		"OCI_URL":                "controller.internal:8080/os-images/windows-wim:22H2",
		"WINDOWS_PARTITION":      "/dev/sda3",
		"IMAGE_APPLY":            "0",
		"IMAGE_WINDOWS_PLAN_BIN": imageWindowsPlan,
		"WINDOWS_MOUNT_PATH":     filepath.Join(tempDir, "new-windows"),
		"WIM_INDEX":              "1",
	})
	assertContains(t, imageOutput, "oras pull", "image plan should include oras pull command")
	assertContains(t, imageOutput, "wimapply", "image plan should include wimapply command")
	assertContains(t, imageOutput, "--index=1", "image plan should include WIM index")

	// Verify bootloader plan
	bootOutput := runWrapper(t, ctx, repoRoot, "bootloader-windows-wrapper.sh", map[string]string{
		"BOOTLOADER_APPLY":            "0",
		"BOOTLOADER_WINDOWS_PLAN_BIN": bootloaderWindowsPlan,
		"ESP_DEVICE":                  "/dev/sda1",
		"WINDOWS_DEVICE":              "/dev/sda3",
		"WINDOWS_MOUNT_PATH":          filepath.Join(tempDir, "new-windows"),
		"ESP_MOUNT_PATH":              filepath.Join(tempDir, "efi"),
		"UNATTEND_XML_FILE":           unattendPath,
	})
	assertContains(t, bootOutput, "efibootmgr", "bootloader plan should include efibootmgr command")
	assertContains(t, bootOutput, "bootmgfw.efi", "bootloader plan should reference Windows bootloader")
	assertContains(t, bootOutput, "Unattend.xml", "bootloader plan should place unattend.xml")
}
