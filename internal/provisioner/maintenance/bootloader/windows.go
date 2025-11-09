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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"shoal/internal/provisioner/maintenance/plan"
)

// WindowsOptions configures the generated command plan for setting up Windows
// UEFI boot on the provisioned system.
type WindowsOptions struct {
	WindowsPath    string
	ESPMountPath   string
	ESPDevice      string
	WindowsDevice  string
	UnattendXML    string
	BootloaderID   string
	BootEntryLabel string
}

// PlanWindows produces the sequence of commands required to:
// 1. Mount ESP and Windows partitions
// 2. Copy Windows boot files to ESP
// 3. Create fallback bootx64.efi
// 4. Create firmware boot entry via efibootmgr
// 5. Place unattend.xml in Windows/Panther
// 6. Clean up mounts
func PlanWindows(opts WindowsOptions) ([]plan.Command, error) {
	windowsPath := opts.WindowsPath
	if windowsPath == "" {
		windowsPath = "/mnt/new-windows"
	}
	windowsPath = filepath.Clean(windowsPath)

	espMount := opts.ESPMountPath
	if espMount == "" {
		espMount = "/mnt/efi"
	}
	espMount = filepath.Clean(espMount)

	if opts.ESPDevice == "" {
		return nil, errors.New("bootloader: ESP device is required")
	}
	espDevice := opts.ESPDevice

	if opts.WindowsDevice == "" {
		return nil, errors.New("bootloader: Windows device is required")
	}
	windowsDevice := opts.WindowsDevice

	if opts.UnattendXML == "" {
		return nil, errors.New("bootloader: unattend.xml content is required")
	}

	bootloaderID := opts.BootloaderID
	if bootloaderID == "" {
		bootloaderID = "Windows Boot Manager"
	}

	bootLabel := opts.BootEntryLabel
	if bootLabel == "" {
		bootLabel = "Windows"
	}

	// Hash unattend.xml for logging (never log content directly)
	unattendHash := sha256.Sum256([]byte(opts.UnattendXML))
	unattendHashStr := hex.EncodeToString(unattendHash[:])

	commands := []plan.Command{
		{
			Program:     "mkdir",
			Args:        []string{"-p", espMount},
			Description: "ensure ESP mount point exists",
		},
		{
			Program:     "mkdir",
			Args:        []string{"-p", windowsPath},
			Description: "ensure Windows mount point exists",
		},
		{
			Program:     "mount",
			Args:        []string{espDevice, espMount},
			Description: "mount EFI system partition",
		},
		{
			Program:     "mount",
			Args:        []string{"-t", "ntfs-3g", windowsDevice, windowsPath},
			Description: "mount Windows partition",
		},
	}

	// Copy Windows boot files from Windows partition to ESP
	windowsBootSrc := filepath.Join(windowsPath, "Windows", "Boot", "EFI")
	espBootDest := filepath.Join(espMount, "EFI", "Microsoft", "Boot")

	commands = append(commands,
		plan.Command{
			Program:     "mkdir",
			Args:        []string{"-p", espBootDest},
			Description: "create EFI boot directory on ESP",
		},
		plan.Command{
			Program:     "cp",
			Args:        []string{"-a", windowsBootSrc + "/.", espBootDest + "/"},
			Description: "copy Windows boot files to ESP",
		},
	)

	// Create fallback bootx64.efi
	espFallbackDir := filepath.Join(espMount, "EFI", "Boot")
	bootmgfwSrc := filepath.Join(espBootDest, "bootmgfw.efi")
	bootx64Dest := filepath.Join(espFallbackDir, "bootx64.efi")

	commands = append(commands,
		plan.Command{
			Program:     "mkdir",
			Args:        []string{"-p", espFallbackDir},
			Description: "create EFI fallback boot directory",
		},
		plan.Command{
			Program:     "cp",
			Args:        []string{bootmgfwSrc, bootx64Dest},
			Description: "create fallback bootx64.efi",
		},
	)

	// Create firmware boot entry (idempotent): skip if label already present
	efibootmgrScript := fmt.Sprintf(`set -euo pipefail
ESP_PART_NUM=$(lsblk -no PARTN %s)
ESP_DISK=$(lsblk -no PKNAME %s)
if [[ -z "$ESP_PART_NUM" ]]; then
	echo "bootloader-windows-plan: unable to determine ESP partition number for %s" >&2
	exit 1
fi
if [[ -z "$ESP_DISK" ]]; then
	echo "bootloader-windows-plan: unable to determine ESP disk for %s" >&2
	exit 1
fi
if efibootmgr | grep -qF %s; then
	echo "bootloader-windows-plan: boot entry '%s' already exists; skipping creation" >&2
	exit 0
fi
efibootmgr --create --disk "/dev/${ESP_DISK}" --part "${ESP_PART_NUM}" \
	--label %s --loader '\EFI\Microsoft\Boot\bootmgfw.efi'
`,
		plan.Quote(espDevice),
		plan.Quote(espDevice),
		plan.Quote(espDevice),
		plan.Quote(espDevice),
		plan.Quote(bootLabel),
		plan.Quote(bootLabel),
		plan.Quote(bootLabel),
	)

	commands = append(commands, plan.Command{
		Program:     "bash",
		Args:        []string{"-c", efibootmgrScript},
		Description: "ensure UEFI boot entry present",
	})

	// Place unattend.xml (hash logged, content never logged)
	unattendDest := filepath.Join(windowsPath, "Windows", "Panther", "Unattend.xml")
	escapedContent := opts.UnattendXML
	// Basic safety: remove potential EOF terminator collisions (extremely unlikely in real XML)
	if strings.Contains(escapedContent, "SHOAL_UNATTEND_EOF") {
		escapedContent = strings.ReplaceAll(escapedContent, "SHOAL_UNATTEND_EOF", "SHOAL_UNATTEND_EOF_1")
	}
	unattendScript := fmt.Sprintf(`set -euo pipefail
UNATTEND_DIR=%s
UNATTEND_FILE=%s
NEW_HASH=%s
mkdir -p "$UNATTEND_DIR"
if [[ -f "$UNATTEND_FILE" ]]; then
  EXISTING_HASH=$(sha256sum "$UNATTEND_FILE" | awk '{print $1}') || true
  if [[ "$EXISTING_HASH" == "$NEW_HASH" ]]; then
	echo "bootloader-windows-plan: unattend.xml unchanged (sha256: %s), skipping write" >&2
	exit 0
  fi
fi
# NOTE: unattend.xml content from recipe (hash: %s, size: %d bytes)
cat > "$UNATTEND_FILE" <<'SHOAL_UNATTEND_EOF'
%s
SHOAL_UNATTEND_EOF
chmod 0600 "$UNATTEND_FILE"
echo "bootloader-windows-plan: unattend.xml written (sha256: %s)" >&2
`,
		plan.Quote(filepath.Dir(unattendDest)),
		plan.Quote(unattendDest),
		unattendHashStr,
		unattendHashStr[:16],
		unattendHashStr[:16],
		len(opts.UnattendXML),
		escapedContent,
		unattendHashStr[:16],
	)

	commands = append(commands, plan.Command{
		Program:     "bash",
		Args:        []string{"-c", unattendScript},
		Description: fmt.Sprintf("ensure unattend.xml present (sha256: %s)", unattendHashStr[:16]),
	})

	// Unmount
	commands = append(commands,
		plan.Command{
			Program:     "umount",
			Args:        []string{windowsPath},
			Description: "unmount Windows partition",
		},
		plan.Command{
			Program:     "umount",
			Args:        []string{espMount},
			Description: "unmount ESP",
		},
	)

	return commands, nil
}
