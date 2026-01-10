//go:build !dev

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dashboard/dist
var distFS embed.FS

// GetFS returns the filesystem containing the dashboard static files.
// In production builds, files are embedded in the binary.
func GetFS() (fs.FS, error) {
	return fs.Sub(distFS, "dashboard/dist")
}
