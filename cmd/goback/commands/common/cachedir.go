package common

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"

	"github.com/dustin/go-humanize"
	"github.com/urfave/cli"
)

// StoreCache is the directory under --cache-dir that holds the caches of
// the store --storage names, or "" when --cache-dir is unset and nothing
// is cached.
func StoreCache(c *cli.Context, name string) string {
	dir := c.GlobalString("cache-dir")
	if dir == "" {
		return ""
	}

	return filepath.Join(dir, "stores", storageID(c), name)
}

// MetadataCacheSize is the --metadata-cache-size in bytes.
func MetadataCacheSize(c *cli.Context) int64 {
	size, err := humanize.ParseBytes(c.GlobalString("metadata-cache-size"))
	if err != nil {
		Fatalf("invalid --metadata-cache-size: %v", err)
	}

	return int64(size)
}

func storageID(c *cli.Context) string {
	sum := sha256.Sum256([]byte(c.GlobalString("storage")))

	return fmt.Sprintf("%x", sum[:8])
}
