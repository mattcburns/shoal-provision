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

package maintenance

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var repoRoot = filepath.Clean("../../..")

func TestShellScriptsPassSyntaxCheck(t *testing.T) {
	scripts := []string{
		"internal/provisioner/maintenance/scripts/partition-wrapper.sh",
		"internal/provisioner/maintenance/scripts/image-linux-wrapper.sh",
		"internal/provisioner/maintenance/scripts/bootloader-linux-wrapper.sh",
		"internal/provisioner/maintenance/scripts/config-drive-wrapper.sh",
		"internal/provisioner/maintenance/scripts/send-webhook.sh",
		"scripts/build_maintenance_os.sh",
	}

	for _, rel := range scripts {
		rel := rel
		t.Run(filepath.Base(rel), func(t *testing.T) {
			if runtime.GOOS != "linux" {
				t.Skip("shell syntax check requires bash on linux")
			}
			path := filepath.Join(repoRoot, rel)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("script missing: %s: %v", path, err)
			}
			cmd := exec.Command("bash", "-n", path)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("bash -n failed: %v\n%s", err, string(out))
			}
		})
	}
}

func TestSystemdUnitsContainExpectedDirectives(t *testing.T) {
	testCases := map[string][]string{
		"internal/provisioner/maintenance/systemd/install-linux.target": {
			"Requires=prepare-provision-mounts.service",
			"OnSuccess=provision-success.service",
			"OnFailure=provision-failed@%n.service",
		},
		"internal/provisioner/maintenance/systemd/partition.service": {
			"EnvironmentFile=/run/provision/recipe.env",
			"ExecStart=/usr/bin/systemctl start partition-tool.service",
		},
		"internal/provisioner/maintenance/systemd/image-linux.service": {
			"ExecStart=/usr/bin/systemctl start image-linux-tool.service",
		},
		"internal/provisioner/maintenance/systemd/bootloader-linux.service": {
			"ExecStart=/usr/bin/systemctl start bootloader-linux-tool.service",
		},
		"internal/provisioner/maintenance/systemd/config-drive.service": {
			"ExecStart=/usr/bin/systemctl start config-drive-tool.service",
		},
		"internal/provisioner/maintenance/systemd/provision-success.service": {
			"StartLimitIntervalSec=10min",
			"StartLimitBurst=10",
			"Restart=on-failure",
			"RestartSec=10s",
		},
		"internal/provisioner/maintenance/systemd/provision-failed@.service": {
			"StartLimitIntervalSec=10min",
			"StartLimitBurst=10",
			"Restart=on-failure",
			"RestartSec=10s",
		},
		"internal/provisioner/maintenance/systemd/provision-dispatcher.service": {
			"ExecStart=/usr/sbin/provisioner --task-iso-device=/dev/sr1 --task-mount-point=/mnt/task --env-dir=/run/provision --log-level=info",
			"OnFailure=provision-failed@%n.service",
		},
	}

	for rel, want := range testCases {
		rel := rel
		t.Run(filepath.Base(rel), func(t *testing.T) {
			path := filepath.Join(repoRoot, rel)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read unit: %v", err)
			}
			text := string(content)
			for _, needle := range want {
				if !strings.Contains(text, needle) {
					t.Fatalf("unit %s missing directive %q", rel, needle)
				}
			}
		})
	}
}

func TestQuadletContainerExecPaths(t *testing.T) {
	check := map[string]string{
		"internal/provisioner/maintenance/quadlet/partition-tool.container":        "Exec=/opt/shoal/bin/partition-wrapper.sh",
		"internal/provisioner/maintenance/quadlet/image-linux-tool.container":      "Exec=/opt/shoal/bin/image-linux-wrapper.sh",
		"internal/provisioner/maintenance/quadlet/bootloader-linux-tool.container": "Exec=/opt/shoal/bin/bootloader-linux-wrapper.sh",
		"internal/provisioner/maintenance/quadlet/config-drive-tool.container":     "Exec=/opt/shoal/bin/config-drive-wrapper.sh",
	}

	for rel, needle := range check {
		rel := rel
		t.Run(filepath.Base(rel), func(t *testing.T) {
			path := filepath.Join(repoRoot, rel)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read container unit: %v", err)
			}
			if !strings.Contains(string(content), needle) {
				t.Fatalf("container unit %s missing %q", rel, needle)
			}
		})
	}
}
