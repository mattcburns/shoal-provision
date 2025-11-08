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
		rootDir    = flag.String("root", "/mnt/new-root", "root filesystem mount point")
		espMount   = flag.String("esp-mount", "/mnt/efi", "mount point for EFI system partition")
		espDevice  = flag.String("esp-device", "", "device path for EFI system partition (required)")
		rootDevice = flag.String("root-device", "", "device path for root filesystem (required)")
		rootFSType = flag.String("root-fs-type", "ext4", "filesystem type for root partition")
		bootID     = flag.String("bootloader-id", "Shoal", "GRUB bootloader identifier")
		grubTarget = flag.String("grub-target", "x86_64-efi", "grub-install --target value")
		output     = flag.String("output", "shell", "output format: shell or json")
	)
	flag.Parse()

	cmds, err := bootloader.Plan(bootloader.Options{
		RootPath:     *rootDir,
		ESPMountPath: *espMount,
		ESPDevice:    *espDevice,
		RootDevice:   *rootDevice,
		RootFSType:   *rootFSType,
		BootloaderID: *bootID,
		GrubTarget:   *grubTarget,
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
	fmt.Fprintf(os.Stderr, "bootloader-plan: "+format+"\n", args...)
	os.Exit(1)
}
