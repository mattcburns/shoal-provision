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
