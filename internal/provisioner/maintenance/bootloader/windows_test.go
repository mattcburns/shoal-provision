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

func TestPlanWindowsGeneratesExpectedSequence(t *testing.T) {
	unattendContent := `<?xml version="1.0" encoding="utf-8"?>
<unattend xmlns="urn:schemas-microsoft-com:unattend">
  <settings pass="specialize">
    <component name="Microsoft-Windows-Shell-Setup">
      <ComputerName>TestPC</ComputerName>
    </component>
  </settings>
</unattend>`

	cmds, err := PlanWindows(WindowsOptions{
		WindowsPath:    "/mnt/new-windows",
		ESPMountPath:   "/mnt/efi",
		ESPDevice:      "/dev/sda1",
		WindowsDevice:  "/dev/sda3",
		UnattendXML:    unattendContent,
		BootloaderID:   "Windows Boot Manager",
		BootEntryLabel: "Windows",
	})
	if err != nil {
		t.Fatalf("PlanWindows returned error: %v", err)
	}

	if len(cmds) == 0 {
		t.Fatalf("expected commands, got none")
	}

	foundMountESP := false
	foundMountWindows := false
	foundCopyBootFiles := false
	foundFallbackBoot := false
	foundEfibootEnsure := false
	foundUnattendXML := false
	foundUmountWindows := false
	foundUmountESP := false

	for _, cmd := range cmds {
		switch cmd.Program {
		case "mount":
			if len(cmd.Args) >= 2 && cmd.Args[0] == "/dev/sda1" && cmd.Args[1] == "/mnt/efi" {
				foundMountESP = true
			}
			if len(cmd.Args) >= 4 && cmd.Args[0] == "-t" && cmd.Args[1] == "ntfs-3g" &&
				cmd.Args[2] == "/dev/sda3" && cmd.Args[3] == "/mnt/new-windows" {
				foundMountWindows = true
			}
		case "cp":
			if len(cmd.Args) == 3 && cmd.Args[0] == "-a" &&
				strings.Contains(cmd.Args[1], "Windows/Boot/EFI") &&
				strings.Contains(cmd.Args[2], "EFI/Microsoft/Boot") {
				foundCopyBootFiles = true
			}
			if len(cmd.Args) == 2 && strings.Contains(cmd.Args[0], "bootmgfw.efi") &&
				strings.Contains(cmd.Args[1], "bootx64.efi") {
				foundFallbackBoot = true
			}
		case "bash":
			if len(cmd.Args) == 2 && cmd.Args[0] == "-c" {
				if strings.Contains(cmd.Args[1], "efibootmgr") &&
					strings.Contains(cmd.Args[1], "--label Windows") &&
					strings.Contains(cmd.Args[1], "bootmgfw.efi") {
					foundEfibootEnsure = true
				}
				if strings.Contains(cmd.Args[1], "Windows/Panther/Unattend.xml") &&
					strings.Contains(cmd.Args[1], "SHOAL_UNATTEND_EOF") &&
					strings.Contains(cmd.Args[1], "ComputerName") {
					foundUnattendXML = true
				}
			}
		case "umount":
			if len(cmd.Args) == 1 && cmd.Args[0] == "/mnt/new-windows" {
				foundUmountWindows = true
			}
			if len(cmd.Args) == 1 && cmd.Args[0] == "/mnt/efi" {
				foundUmountESP = true
			}
		}
	}

	if !foundMountESP {
		t.Errorf("expected mount of ESP device in plan")
	}
	if !foundMountWindows {
		t.Errorf("expected mount of Windows device in plan")
	}
	if !foundCopyBootFiles {
		t.Errorf("expected copy of Windows boot files to ESP")
	}
	if !foundFallbackBoot {
		t.Errorf("expected creation of fallback bootx64.efi")
	}
	if !foundEfibootEnsure {
		t.Errorf("expected efibootmgr command to ensure boot entry")
	}
	if !foundUnattendXML {
		t.Errorf("expected unattend.xml placement command")
	}
	if !foundUmountWindows {
		t.Errorf("expected umount of Windows partition")
	}
	if !foundUmountESP {
		t.Errorf("expected final umount of ESP")
	}
}

func TestPlanWindowsUnattendSecurityNoContentInLogs(t *testing.T) {
	sensitiveContent := `<?xml version="1.0"?>
<unattend><settings><component><UserData><Password>SuperSecret123!</Password></UserData></component></settings></unattend>`

	cmds, err := PlanWindows(WindowsOptions{
		ESPDevice:     "/dev/sda1",
		WindowsDevice: "/dev/sda3",
		UnattendXML:   sensitiveContent,
	})
	if err != nil {
		t.Fatalf("PlanWindows returned error: %v", err)
	}

	// Verify that the password doesn't appear in any command description
	for _, cmd := range cmds {
		if strings.Contains(cmd.Description, "SuperSecret123!") {
			t.Errorf("unattend.xml password found in description: %s", cmd.Description)
		}
	}

	// The unattend script should contain the content (it's in the bash script)
	foundUnattendScript := false
	for _, cmd := range cmds {
		if cmd.Program == "bash" && len(cmd.Args) == 2 && cmd.Args[0] == "-c" {
			if strings.Contains(cmd.Args[1], "SHOAL_UNATTEND_EOF") {
				foundUnattendScript = true
				// Script must contain the actual content
				if !strings.Contains(cmd.Args[1], "SuperSecret123!") {
					t.Errorf("unattend.xml content not found in script")
				}
				// But description should only have hash
				if strings.Contains(cmd.Description, "SuperSecret123!") {
					t.Errorf("unattend.xml password found in description")
				}
			}
		}
	}

	if !foundUnattendScript {
		t.Errorf("expected unattend.xml script in commands")
	}
}

func TestPlanWindowsUnattendIdempotentSkip(t *testing.T) {
	content := "<unattend><settings><component><ComputerName>SkipTest</ComputerName></component></settings></unattend>"
	cmds, err := PlanWindows(WindowsOptions{ESPDevice: "/dev/sda1", WindowsDevice: "/dev/sda3", UnattendXML: content})
	if err != nil {
		t.Fatalf("PlanWindows returned error: %v", err)
	}
	foundScript := false
	foundSkipLogic := false
	for _, cmd := range cmds {
		if cmd.Program == "bash" && len(cmd.Args) == 2 && cmd.Args[0] == "-c" {
			foundScript = true
			if strings.Contains(cmd.Args[1], "unattend.xml unchanged") && strings.Contains(cmd.Args[1], "EXISTING_HASH") {
				foundSkipLogic = true
			}
		}
	}
	if !foundScript {
		t.Fatalf("expected unattend script in commands")
	}
	if !foundSkipLogic {
		t.Fatalf("expected skip logic for unchanged unattend.xml")
	}
}

func TestPlanWindowsValidatesRequiredFields(t *testing.T) {
	_, err := PlanWindows(WindowsOptions{})
	if err == nil {
		t.Fatalf("expected error for missing devices")
	}

	_, err = PlanWindows(WindowsOptions{ESPDevice: "/dev/sda1"})
	if err == nil {
		t.Fatalf("expected error when Windows device missing")
	}

	_, err = PlanWindows(WindowsOptions{
		ESPDevice:     "/dev/sda1",
		WindowsDevice: "/dev/sda3",
	})
	if err == nil {
		t.Fatalf("expected error when unattend.xml missing")
	}
}

func TestPlanWindowsDefaultValues(t *testing.T) {
	cmds, err := PlanWindows(WindowsOptions{
		ESPDevice:     "/dev/sda1",
		WindowsDevice: "/dev/sda3",
		UnattendXML:   "<unattend></unattend>",
		// All other fields default
	})
	if err != nil {
		t.Fatalf("PlanWindows returned error: %v", err)
	}

	// Verify default paths
	foundDefaultWinPath := false
	foundDefaultESPPath := false

	for _, cmd := range cmds {
		cmdStr := cmd.Shell()
		if strings.Contains(cmdStr, "/mnt/new-windows") {
			foundDefaultWinPath = true
		}
		if strings.Contains(cmdStr, "/mnt/efi") {
			foundDefaultESPPath = true
		}
	}

	if !foundDefaultWinPath {
		t.Errorf("expected default Windows path /mnt/new-windows")
	}
	if !foundDefaultESPPath {
		t.Errorf("expected default ESP path /mnt/efi")
	}
}
