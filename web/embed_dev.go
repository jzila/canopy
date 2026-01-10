//go:build dev

package web

import (
	"io/fs"
	"os"
)

// GetFS returns the filesystem containing the dashboard static files.
// In development builds (with -tags dev), files are read from disk
// to allow live reloading without recompiling.
func GetFS() (fs.FS, error) {
	return os.DirFS("web/dashboard/dist"), nil
}
