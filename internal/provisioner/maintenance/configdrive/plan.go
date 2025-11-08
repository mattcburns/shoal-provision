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
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"shoal/internal/provisioner/maintenance/plan"
)

// Options configures the plan for populating a NoCloud Config Drive partition.
type Options struct {
	MountPath    string
	Device       string
	UserDataPath string
	MetaDataPath string
	InstanceID   string
	Hostname     string
}

// Plan returns the set of commands required to mount the Config Drive partition
// and populate the expected cloud-init files.
func Plan(opts Options) ([]plan.Command, error) {
	mountPath := opts.MountPath
	if mountPath == "" {
		mountPath = "/mnt/cidata"
	}
	mountPath = filepath.Clean(mountPath)

	if opts.Device == "" {
		return nil, errors.New("configdrive: device is required")
	}
	device := opts.Device

	instanceID := strings.TrimSpace(opts.InstanceID)
	if instanceID == "" {
		instanceID = "shoal-instance"
	}

	hostname := strings.TrimSpace(opts.Hostname)
	if hostname == "" {
		hostname = "shoal-host"
	}

	commands := []plan.Command{
		{
			Program:     "mkdir",
			Args:        []string{"-p", mountPath},
			Description: "ensure CIDATA mount point exists",
		},
		{
			Program:     "mount",
			Args:        []string{device, mountPath},
			Description: "mount CIDATA partition",
		},
	}

	script := fmt.Sprintf(`set -euo pipefail
CIDATA=%s
USER_SRC=%s
META_SRC=%s
INSTANCE_ID=%s
HOSTNAME=%s

if [[ ! -d "$CIDATA" ]]; then
  echo "config-drive-plan: mount path $CIDATA not accessible" >&2
  exit 1
fi

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmpdir"
}
trap cleanup EXIT

if [[ -n "$USER_SRC" && -f "$USER_SRC" ]]; then
  install -m 0644 "$USER_SRC" "$tmpdir/user-data"
fi

if [[ -n "$META_SRC" && -f "$META_SRC" ]]; then
  install -m 0644 "$META_SRC" "$tmpdir/meta-data"
else
  printf 'instance-id: %%s\nlocal-hostname: %%s\n' "$INSTANCE_ID" "$HOSTNAME" > "$tmpdir/meta-data"
fi

install -m 0755 -d "$CIDATA"
if [[ -f "$tmpdir/user-data" ]]; then
  install -m 0644 "$tmpdir/user-data" "$CIDATA/user-data"
else
  rm -f "$CIDATA/user-data"
fi
install -m 0644 "$tmpdir/meta-data" "$CIDATA/meta-data"
sync
`,
		plan.Quote(mountPath),
		plan.Quote(opts.UserDataPath),
		plan.Quote(opts.MetaDataPath),
		plan.Quote(instanceID),
		plan.Quote(hostname),
	)

	commands = append(commands, plan.Command{
		Program:     "bash",
		Args:        []string{"-c", script},
		Description: "populate cloud-init user-data and meta-data",
	})

	commands = append(commands, plan.Command{
		Program:     "umount",
		Args:        []string{mountPath},
		Description: "unmount CIDATA partition",
	})

	return commands, nil
}
