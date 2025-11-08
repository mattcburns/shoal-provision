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

// Options configures the generated commands.
type Options struct {
	OCIURL   string
	RootPath string
}

// Plan returns the set of commands required to extract an OCI-hosted rootfs
// tarball into the target root directory using oras and tar.
func Plan(opts Options) ([]plan.Command, error) {
	url := strings.TrimSpace(opts.OCIURL)
	if url == "" {
		return nil, errors.New("image: OCI URL is required")
	}

	root := opts.RootPath
	if root == "" {
		root = "/mnt/new-root"
	}
	root = filepath.Clean(root)

	commands := []plan.Command{
		{
			Program:     "mkdir",
			Args:        []string{"-p", root},
			Description: "ensure root mount directory exists",
		},
		{
			Program: "bash",
			Args: []string{"-c", fmt.Sprintf(
				"oras pull %s --output - | tar -xpf - -C %s",
				plan.Quote(url), plan.Quote(root),
			)},
			Description: "stream root filesystem tarball",
		},
	}

	return commands, nil
}
