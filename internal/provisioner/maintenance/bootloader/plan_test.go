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

package bootloader

import (
	"strings"
	"testing"
)

func TestPlanGeneratesExpectedSequence(t *testing.T) {
	cmds, err := Plan(Options{
		RootPath:     "/mnt/new-root",
		ESPMountPath: "/mnt/efi",
		ESPDevice:    "/dev/sda1",
		RootDevice:   "/dev/sda2",
		RootFSType:   "ext4",
		BootloaderID: "Shoal",
		GrubTarget:   "x86_64-efi",
	})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	if len(cmds) == 0 {
		t.Fatalf("expected commands, got none")
	}

	foundMountESP := false
	foundGrubInstall := false
	foundFstab := false
	foundFinalUmount := false

	for _, cmd := range cmds {
		switch cmd.Program {
		case "mount":
			if len(cmd.Args) >= 2 && cmd.Args[0] == "/dev/sda1" && cmd.Args[1] == "/mnt/efi" {
				foundMountESP = true
			}
		case "chroot":
			if len(cmd.Args) >= 2 && cmd.Args[1] == "grub-install" {
				foundGrubInstall = true
			}
		case "bash":
			if len(cmd.Args) == 2 && cmd.Args[0] == "-c" &&
				strings.Contains(cmd.Args[1], "blkid -s PARTUUID -o value") &&
				strings.Contains(cmd.Args[1], "/dev/sda2") &&
				strings.Contains(cmd.Args[1], "/dev/sda1") &&
				strings.Contains(cmd.Args[1], "PARTUUID=$ROOT_UUID / ext4") &&
				strings.Contains(cmd.Args[1], "PARTUUID=$ESP_UUID /boot/efi") {
				foundFstab = true
			}
		case "umount":
			if len(cmd.Args) == 1 && cmd.Args[0] == "/mnt/efi" {
				foundFinalUmount = true
			}
		}
	}

	if !foundMountESP {
		t.Errorf("expected mount of ESP device in plan")
	}
	if !foundGrubInstall {
		t.Errorf("expected grub-install chroot command in plan")
	}
	if !foundFstab {
		t.Errorf("expected /etc/fstab generation command in plan")
	}
	if !foundFinalUmount {
		t.Errorf("expected final umount of ESP mount point")
	}
}

func TestPlanValidatesRequiredFields(t *testing.T) {
	_, err := Plan(Options{})
	if err == nil {
		t.Fatalf("expected error for missing devices")
	}

	_, err = Plan(Options{ESPDevice: "/dev/sda1"})
	if err == nil {
		t.Fatalf("expected error when root device missing")
	}
}
