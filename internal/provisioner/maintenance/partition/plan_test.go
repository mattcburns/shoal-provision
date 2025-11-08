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

package partition

import (
	"strings"
	"testing"
)

func TestPlanGeneratesExpectedCommands(t *testing.T) {
	layout := `[
		{"size":"512M","type_guid":"ef00","format":"vfat","label":"EFI"},
		{"size":"32G","type_guid":"8200","format":"swap","label":"swap"},
		{"size":"100%","type_guid":"8300","format":"ext4","label":"root"}
	]`

	cmds, err := Plan("/dev/nvme0n1", []byte(layout))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	expect := []struct {
		program  string
		fragment string
	}{
		{"sgdisk", "--zap-all /dev/nvme0n1"},
		{"sgdisk", "-o /dev/nvme0n1"},
		{"sgdisk", "-n 1:0:+512M /dev/nvme0n1"},
		{"sgdisk", "-t 1:C12A7328-F81F-11D2-BA4B-00A0C93EC93B /dev/nvme0n1"},
		{"sgdisk", "-c 1:EFI /dev/nvme0n1"},
		{"mkfs.vfat", "-F 32 -n EFI /dev/nvme0n1p1"},
		{"sgdisk", "-n 2:0:+32G /dev/nvme0n1"},
		{"sgdisk", "-t 2:0657FD6D-A4AB-43C4-84E5-0933C84B4F4F /dev/nvme0n1"},
		{"sgdisk", "-c 2:swap /dev/nvme0n1"},
		{"mkswap", "-L swap /dev/nvme0n1p2"},
		{"sgdisk", "-n 3:0:0 /dev/nvme0n1"},
		{"sgdisk", "-t 3:0FC63DAF-8483-4772-8E79-3D69D8477DE4 /dev/nvme0n1"},
		{"sgdisk", "-c 3:root /dev/nvme0n1"},
		{"mkfs.ext4", "-F -L root /dev/nvme0n1p3"},
		{"sgdisk", "-p /dev/nvme0n1"},
	}

	if len(cmds) != len(expect) {
		t.Fatalf("expected %d commands, got %d", len(expect), len(cmds))
	}

	for i, want := range expect {
		got := cmds[i]
		if got.Program != want.program {
			t.Fatalf("command %d: program mismatch: want %s got %s", i, want.program, got.Program)
		}
		joined := got.Shell()
		if !strings.Contains(joined, want.fragment) {
			t.Fatalf("command %d: expected fragment %q in %q", i, want.fragment, joined)
		}
	}
}

func TestUnknownFormatFails(t *testing.T) {
	layout := `[{"size":"1G","type_guid":"8300","format":"zfs"}]`
	if _, err := Plan("/dev/sda", []byte(layout)); err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

func TestAliasValidation(t *testing.T) {
	layout := `[{"size":"1G","type_guid":"C12A7328-F81F-11D2-BA4B-00A0C93EC93B"}]`
	if _, err := Plan("/dev/sda", []byte(layout)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPartitionDeviceName(t *testing.T) {
	if got := partitionDeviceName("/dev/sda", 1); got != "/dev/sda1" {
		t.Fatalf("unexpected device name: %s", got)
	}
	if got := partitionDeviceName("/dev/nvme0n1", 3); got != "/dev/nvme0n1p3" {
		t.Fatalf("unexpected device name for nvme: %s", got)
	}
}

func TestWindowsLayout_EFI_MSR_NTFS(t *testing.T) {
	layout := `[
		{"size":"512M","type_guid":"ef00","format":"vfat","label":"EFI"},
		{"size":"16M","type_guid":"0c01","format":"raw","label":"MSR"},
		{"size":"100%","type_guid":"0700","format":"ntfs","label":"Windows"}
	]`

	cmds, err := Plan("/dev/sda", []byte(layout))
	if err != nil {
		t.Fatalf("Plan returned error for Windows layout: %v", err)
	}

	// Verify key commands are present
	expectFragments := []string{
		"--zap-all",
		"-o /dev/sda", // GPT init
		"-n 1:0:+512M",
		"-t 1:C12A7328-F81F-11D2-BA4B-00A0C93EC93B", // EFI
		"mkfs.vfat",
		"-n 2:0:+16M",
		"-t 2:E3C9E316-0B5C-4DB8-817D-F92DF00215AE", // MSR
		"-n 3:0:0",
		"-t 3:EBD0A0A2-B9E5-4433-87C0-68B6B72699C7", // Windows data
		"mkfs.ntfs",
	}

	joined := ""
	for _, cmd := range cmds {
		joined += cmd.Shell() + " "
	}

	for _, frag := range expectFragments {
		if !strings.Contains(joined, frag) {
			t.Fatalf("expected fragment %q not found in command output", frag)
		}
	}
}

func TestMSRPartition_NoFilesystem(t *testing.T) {
	layout := `[{"size":"16M","type_guid":"0c01","format":"raw"}]`

	cmds, err := Plan("/dev/sda", []byte(layout))
	if err != nil {
		t.Fatalf("Plan returned error for MSR partition: %v", err)
	}

	// Ensure no mkfs command is generated for raw format
	for _, cmd := range cmds {
		if strings.HasPrefix(cmd.Program, "mkfs") || cmd.Program == "mkswap" {
			t.Fatalf("unexpected format command for raw partition: %s", cmd.Shell())
		}
	}

	// Verify MSR type GUID is set correctly
	joined := ""
	for _, cmd := range cmds {
		joined += cmd.Shell() + " "
	}
	if !strings.Contains(joined, "E3C9E316-0B5C-4DB8-817D-F92DF00215AE") {
		t.Fatal("MSR partition type GUID not found in commands")
	}
}

func TestNTFSFormatSupported(t *testing.T) {
	layout := `[{"size":"50G","type_guid":"0700","format":"ntfs","label":"Data"}]`

	cmds, err := Plan("/dev/sda", []byte(layout))
	if err != nil {
		t.Fatalf("Plan should support ntfs format: %v", err)
	}

	// Verify mkfs.ntfs command is generated
	found := false
	for _, cmd := range cmds {
		if cmd.Program == "mkfs.ntfs" {
			found = true
			if !strings.Contains(cmd.Shell(), "-f") || !strings.Contains(cmd.Shell(), "-F") {
				t.Fatal("mkfs.ntfs should include -f and -F flags")
			}
			if !strings.Contains(cmd.Shell(), "-L Data") {
				t.Fatal("mkfs.ntfs should include label")
			}
		}
	}
	if !found {
		t.Fatal("expected mkfs.ntfs command not found")
	}
}
