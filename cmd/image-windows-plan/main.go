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

	"shoal/internal/provisioner/maintenance/image"
)

func main() {
	var (
		ociURL    = flag.String("oci-url", "", "OCI reference to WIM image (required)")
		winPath   = flag.String("windows-path", "/mnt/new-windows", "Windows filesystem mount point")
		wimIndex  = flag.Int("wim-index", 1, "WIM image index to apply")
		partition = flag.String("partition", "", "NTFS partition device (e.g. /dev/sda3) (required)")
		output    = flag.String("output", "shell", "output format: shell or json")
	)
	flag.Parse()

	cmds, err := image.PlanWindows(image.WindowsOptions{
		OCIURL:       *ociURL,
		WindowsPath:  *winPath,
		WIMIndex:     *wimIndex,
		PartitionDev: *partition,
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
	fmt.Fprintf(os.Stderr, "image-windows-plan: "+format+"\n", args...)
	os.Exit(1)
}
