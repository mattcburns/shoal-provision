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

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"shoal/internal/provisioner/maintenance/bootloader"
)

func main() {
	var (
		windowsPath    = flag.String("windows-path", "/mnt/new-windows", "Windows filesystem mount point")
		espMount       = flag.String("esp-mount", "/mnt/efi", "ESP mount point")
		espDevice      = flag.String("esp-device", "", "ESP partition device (e.g. /dev/sda1) (required)")
		windowsDevice  = flag.String("windows-device", "", "Windows partition device (e.g. /dev/sda3) (required)")
		unattendXML    = flag.String("unattend-xml", "", "Unattend.xml content (required)")
		bootloaderID   = flag.String("bootloader-id", "Windows Boot Manager", "Bootloader ID")
		bootEntryLabel = flag.String("boot-entry-label", "Windows", "UEFI boot entry label")
		output         = flag.String("output", "shell", "output format: shell or json")
	)
	flag.Parse()

	cmds, err := bootloader.PlanWindows(bootloader.WindowsOptions{
		WindowsPath:    *windowsPath,
		ESPMountPath:   *espMount,
		ESPDevice:      *espDevice,
		WindowsDevice:  *windowsDevice,
		UnattendXML:    *unattendXML,
		BootloaderID:   *bootloaderID,
		BootEntryLabel: *bootEntryLabel,
	})
	if err != nil {
		fatalf("plan: %v", err)
	}

	switch *output {
	case "shell":
		for _, cmd := range cmds {
			fmt.Println(cmd.Shell())
		}
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(cmds); err != nil {
			fatalf("encode json: %v", err)
		}
	default:
		fatalf("unknown output format %q", *output)
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "bootloader-windows-plan: "+format+"\n", args...)
	os.Exit(1)
}
