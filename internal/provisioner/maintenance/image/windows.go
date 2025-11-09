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

package image

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"shoal/internal/provisioner/maintenance/plan"
)

// WindowsOptions configures the WIM imaging commands.
type WindowsOptions struct {
	OCIURL       string
	WindowsPath  string
	WIMIndex     int
	PartitionDev string
}

// PlanWindows returns the set of commands required to mount an NTFS partition,
// pull a WIM image from OCI, and apply it to the Windows partition using wimapply.
func PlanWindows(opts WindowsOptions) ([]plan.Command, error) {
	url := strings.TrimSpace(opts.OCIURL)
	if url == "" {
		return nil, errors.New("image: OCI URL is required")
	}

	winPath := opts.WindowsPath
	if winPath == "" {
		winPath = "/mnt/new-windows"
	}
	winPath = filepath.Clean(winPath)

	partDev := strings.TrimSpace(opts.PartitionDev)
	if partDev == "" {
		return nil, errors.New("image: partition device is required")
	}

	index := opts.WIMIndex
	if index < 1 {
		index = 1
	}

	// Idempotency strategy:
	// 1. Compute manifest digest of the OCI reference (lightweight) via oras manifest fetch
	// 2. Compare with stamp file "$WIN_PATH/.provisioner_wim_digest"
	// 3. If unchanged, skip wimapply; otherwise stream apply and update stamp.
	// NOTE: Using manifest digest rather than full WIM file hash keeps memory/time lower; acceptable for change detection.
	// LIMITATION: If a blob were replaced in-place in the registry without a manifest change (non-standard OCI practice),
	// the digest comparison would not detect it. This tradeoff is intentional for performance; full WIM hashing is costly.
	idempotentScript := fmt.Sprintf(`set -euo pipefail
WIN_PATH=%s
OCI_REF=%s
WIM_INDEX=%d
STAMP_FILE="$WIN_PATH/.provisioner_wim_digest"
mkdir -p "$WIN_PATH"

# Mount partition if not already mounted
MOUNTED=0
if mountpoint -q "$WIN_PATH"; then
  echo "image-windows-plan: $WIN_PATH already mounted" >&2
else
  if ! mount -t ntfs-3g %s "$WIN_PATH"; then
    echo "image-windows-plan: failed to mount %s on $WIN_PATH" >&2
    exit 1
  fi
  MOUNTED=1
fi

CURRENT_DIGEST=$(oras manifest fetch "$OCI_REF" 2>/dev/null | sha256sum | awk '{print $1}') || {
  echo "image-windows-plan: failed to fetch manifest for $OCI_REF" >&2
  [ "$MOUNTED" = "1" ] && umount "$WIN_PATH"
  exit 1
}

if [ -f "$STAMP_FILE" ]; then
  PREV_DIGEST=$(cat "$STAMP_FILE")
  if [ "$PREV_DIGEST" = "$CURRENT_DIGEST" ]; then
	echo "image-windows-plan: WIM digest unchanged ($CURRENT_DIGEST), skipping apply" >&2
	[ "$MOUNTED" = "1" ] && umount "$WIN_PATH"
	exit 0
  fi
fi

echo "image-windows-plan: applying WIM (index=$WIM_INDEX, digest=$CURRENT_DIGEST)" >&2
if ! oras pull "$OCI_REF" --output - | wimapply - "$WIN_PATH" --index=%d; then
  echo "image-windows-plan: wimapply failed" >&2
  [ "$MOUNTED" = "1" ] && umount "$WIN_PATH"
  exit 1
fi

echo "$CURRENT_DIGEST" > "$STAMP_FILE"
sync
[ "$MOUNTED" = "1" ] && umount "$WIN_PATH"
`,
		plan.Quote(winPath), plan.Quote(url), index, plan.Quote(partDev), plan.Quote(partDev), index)

	commands := []plan.Command{
		{
			Program:     "bash",
			Args:        []string{"-c", idempotentScript},
			Description: fmt.Sprintf("idempotent WIM apply (index %d) with digest stamp", index),
		},
	}

	return commands, nil
}
