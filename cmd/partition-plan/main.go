// Shoal is a Redfish aggregator service.package partitionplan

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
	"io"
	"os"

	"shoal/internal/provisioner/maintenance/partition"
)

func main() {
	var (
		disk   = flag.String("disk", "", "target disk device (e.g. /dev/sda)")
		layout = flag.String("layout", "", "path to layout.json (default: stdin)")
		output = flag.String("output", "shell", "output format: shell or json")
	)
	flag.Parse()

	if *disk == "" {
		fatalf("--disk is required")
	}

	var data []byte
	var err error
	if *layout != "" {
		data, err = os.ReadFile(*layout)
	} else {
		data, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		fatalf("read layout: %v", err)
	}

	cmds, err := partition.Plan(*disk, data)
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
	fmt.Fprintf(os.Stderr, "partition-plan: "+format+"\n", args...)
	os.Exit(1)
}
