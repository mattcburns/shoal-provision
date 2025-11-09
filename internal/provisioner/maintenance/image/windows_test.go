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

package image

import (
	"strings"
	"testing"
)

func TestPlanWindows(t *testing.T) {
	cmds, err := PlanWindows(WindowsOptions{
		OCIURL:       "controller:8080/os-images/windows-wim:22H2",
		WindowsPath:  "/mnt/new-windows",
		WIMIndex:     1,
		PartitionDev: "/dev/sda3",
	})
	if err != nil {
		t.Fatalf("PlanWindows returned error: %v", err)
	}
	if len(cmds) < 1 {
		t.Fatalf("expected at least one command, got %d", len(cmds))
	}

	// Single bash script should contain oras pull, wimapply, --index=1, and mount/umount
	sh := cmds[0].Shell()
	if !(strings.Contains(sh, "oras pull") && strings.Contains(sh, "OCI_REF=controller:8080/os-images/windows-wim:22H2")) {
		t.Fatalf("expected oras pull with OCI_REF in script: %s", sh)
	}
	if !(strings.Contains(sh, "wimapply -") && strings.Contains(sh, "--index=1") && strings.Contains(sh, "WIN_PATH=/mnt/new-windows")) {
		t.Fatalf("expected wimapply with index and WIN_PATH in script: %s", sh)
	}
	if !(strings.Contains(sh, "mount -t ntfs-3g /dev/sda3 \"$WIN_PATH\"") || strings.Contains(sh, "mount -t ntfs-3g /dev/sda3 /mnt/new-windows")) {
		t.Fatalf("expected mount in script: %s", sh)
	}
	if !(strings.Contains(sh, "umount \"$WIN_PATH\"") || strings.Contains(sh, "umount /mnt/new-windows")) {
		t.Fatalf("expected umount in script: %s", sh)
	}
}

func TestPlanWindowsWithCustomIndex(t *testing.T) {
	cmds, err := PlanWindows(WindowsOptions{
		OCIURL:       "controller:8080/os-images/windows-wim:22H2",
		WindowsPath:  "/mnt/new-windows",
		WIMIndex:     3,
		PartitionDev: "/dev/sda3",
	})
	if err != nil {
		t.Fatalf("PlanWindows returned error: %v", err)
	}

	// Verify index is used
	wimCmd := cmds[0].Shell()
	if !strings.Contains(wimCmd, "--index=3") {
		t.Fatalf("expected --index=3 in wimapply command: %s", wimCmd)
	}
}

func TestPlanWindowsDefaultIndex(t *testing.T) {
	cmds, err := PlanWindows(WindowsOptions{
		OCIURL:       "controller:8080/os-images/windows-wim:22H2",
		WindowsPath:  "/mnt/new-windows",
		WIMIndex:     0, // Invalid, should default to 1
		PartitionDev: "/dev/sda3",
	})
	if err != nil {
		t.Fatalf("PlanWindows returned error: %v", err)
	}

	// Verify defaults to index 1
	wimCmd := cmds[0].Shell()
	if !strings.Contains(wimCmd, "--index=1") {
		t.Fatalf("expected --index=1 (default) in wimapply command: %s", wimCmd)
	}
}

func TestPlanWindowsRequiresURL(t *testing.T) {
	if _, err := PlanWindows(WindowsOptions{PartitionDev: "/dev/sda3"}); err == nil {
		t.Fatalf("expected error for missing OCI URL")
	}
}

func TestPlanWindowsRequiresPartition(t *testing.T) {
	if _, err := PlanWindows(WindowsOptions{OCIURL: "controller:8080/os-images/windows-wim:22H2"}); err == nil {
		t.Fatalf("expected error for missing partition device")
	}
}

func TestPlanWindowsDefaultPath(t *testing.T) {
	cmds, err := PlanWindows(WindowsOptions{
		OCIURL:       "controller:8080/os-images/windows-wim:22H2",
		PartitionDev: "/dev/sda3",
		// WindowsPath not specified, should default to /mnt/new-windows
	})
	if err != nil {
		t.Fatalf("PlanWindows returned error: %v", err)
	}

	// Verify default path is used in script
	script := cmds[0].Shell()
	if !strings.Contains(script, "/mnt/new-windows") {
		t.Fatalf("expected default path /mnt/new-windows in script: %s", script)
	}
}
