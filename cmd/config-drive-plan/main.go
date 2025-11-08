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

	"shoal/internal/provisioner/maintenance/configdrive"
)

func main() {
	var (
		mountPath  = flag.String("mount", "/mnt/cidata", "mount point for CIDATA partition")
		device     = flag.String("device", "", "device path for CIDATA partition (required unless omitted)")
		userData   = flag.String("user-data", "/run/provision/user-data", "user-data source file")
		metaData   = flag.String("meta-data", "/run/provision/meta-data", "meta-data source file")
		instanceID = flag.String("instance-id", "shoal-instance", "instance-id for generated meta-data")
		hostname   = flag.String("hostname", "shoal-host", "hostname for generated meta-data")
		output     = flag.String("output", "shell", "output format: shell or json")
	)
	flag.Parse()

	cmds, err := configdrive.Plan(configdrive.Options{
		MountPath:    *mountPath,
		Device:       *device,
		UserDataPath: *userData,
		MetaDataPath: *metaData,
		InstanceID:   *instanceID,
		Hostname:     *hostname,
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
	fmt.Fprintf(os.Stderr, "config-drive-plan: "+format+"\n", args...)
	os.Exit(1)
}
