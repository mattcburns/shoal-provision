// Shoal is a Redfish aggregator service.
// Copyright (C) 2025  Matthew Burns
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

// Package assets provides embedded static files for the Shoal application.
// This allows the application to be distributed as a single binary file.
package assets

import (
	"embed"
	"io/fs"
)

// Embed the static directory contents
// Use all:static to include all files including those starting with .
// and to handle empty directories gracefully
//
//go:embed all:static
var staticFiles embed.FS

// GetStaticFS returns the embedded filesystem for static files
func GetStaticFS() fs.FS {
	// Strip the "static/" prefix so paths work as expected
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic("failed to get static subdirectory: " + err.Error())
	}
	return sub
}
