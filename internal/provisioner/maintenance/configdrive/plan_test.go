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

package configdrive

import (
	"strings"
	"testing"
)

func TestPlanIncludesExpectedCommands(t *testing.T) {
	cmds, err := Plan(Options{
		MountPath:    "/mnt/cidata",
		Device:       "/dev/sda3",
		UserDataPath: "/run/provision/user-data",
		MetaDataPath: "/run/provision/meta-data",
		InstanceID:   "node-123",
		Hostname:     "server01",
	})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(cmds) < 3 {
		t.Fatalf("expected at least 3 commands, got %d", len(cmds))
	}

	foundMount := false
	foundScript := false
	foundUmount := false
	for _, cmd := range cmds {
		switch cmd.Program {
		case "mount":
			if len(cmd.Args) == 2 && cmd.Args[0] == "/dev/sda3" && cmd.Args[1] == "/mnt/cidata" {
				foundMount = true
			}
		case "bash":
			if len(cmd.Args) == 2 && cmd.Args[0] == "-c" &&
				strings.Contains(cmd.Args[1], "instance-id:") &&
				strings.Contains(cmd.Args[1], "local-hostname:") {
				foundScript = true
			}
		case "umount":
			if len(cmd.Args) == 1 && cmd.Args[0] == "/mnt/cidata" {
				foundUmount = true
			}
		}
	}

	if !foundMount {
		t.Errorf("expected mount command in plan")
	}
	if !foundScript {
		t.Errorf("expected script command generating meta-data")
	}
	if !foundUmount {
		t.Errorf("expected umount command in plan")
	}
}

func TestPlanRequiresDevice(t *testing.T) {
	if _, err := Plan(Options{}); err == nil {
		t.Fatalf("expected error when device missing")
	}
}
