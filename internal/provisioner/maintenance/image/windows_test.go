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
	if len(cmds) != 4 {
		t.Fatalf("expected 4 commands, got %d", len(cmds))
	}

	// Verify mkdir
	if got := cmds[0].Shell(); got != "mkdir -p /mnt/new-windows" {
		t.Fatalf("unexpected mkdir command: %s", got)
	}

	// Verify mount
	if got := cmds[1].Shell(); got != "mount -t ntfs-3g /dev/sda3 /mnt/new-windows" {
		t.Fatalf("unexpected mount command: %s", got)
	}

	// Verify wimapply stream
	want := "bash -c 'oras pull controller:8080/os-images/windows-wim:22H2 --output - | wimapply - /mnt/new-windows --index=1'"
	if got := cmds[2].Shell(); got != want {
		t.Fatalf("unexpected stream command:\n got: %s\nwant: %s", got, want)
	}

	// Verify umount
	if got := cmds[3].Shell(); got != "umount /mnt/new-windows" {
		t.Fatalf("unexpected umount command: %s", got)
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
	wimCmd := cmds[2].Shell()
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
	wimCmd := cmds[2].Shell()
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

	// Verify default path is used
	mkdirCmd := cmds[0].Shell()
	if !strings.Contains(mkdirCmd, "/mnt/new-windows") {
		t.Fatalf("expected default path /mnt/new-windows in mkdir: %s", mkdirCmd)
	}
}
