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

	commands := []plan.Command{
		{
			Program:     "mkdir",
			Args:        []string{"-p", winPath},
			Description: "ensure Windows mount directory exists",
		},
		{
			Program:     "mount",
			Args:        []string{"-t", "ntfs-3g", partDev, winPath},
			Description: fmt.Sprintf("mount NTFS partition %s to %s", partDev, winPath),
		},
		{
			Program: "bash",
			Args: []string{"-c", fmt.Sprintf(
				"oras pull %s --output - | wimapply - %s --index=%d",
				plan.Quote(url), plan.Quote(winPath), index,
			)},
			Description: fmt.Sprintf("stream WIM image and apply index %d", index),
		},
		{
			Program:     "umount",
			Args:        []string{winPath},
			Description: fmt.Sprintf("unmount %s", winPath),
		},
	}

	return commands, nil
}
