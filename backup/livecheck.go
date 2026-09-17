package backup

import (
	"io/fs"
	"path/filepath"
)

const liveMarker = "session.lock"

// LiveMarkers returns the session lock files under root that another
// process holds, which is how a running game server shows itself.
func LiveMarkers(root string) ([]string, error) {
	var held []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if !d.IsDir() && d.Name() == liveMarker && lockedElsewhere(path) {
			held = append(held, path)
		}

		return nil
	})

	return held, err
}
