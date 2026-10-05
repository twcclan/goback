package common

import (
	"log"
	"path/filepath"

	"github.com/twcclan/goback/backup/blobcache"
	"github.com/twcclan/goback/backup/storekey"

	"github.com/dustin/go-humanize"
	"github.com/urfave/cli"
)

// BlobCache opens the blob cache under --cache-dir for the store the
// command works against, or returns nil when --cache-dir is unset.
func BlobCache(c *cli.Context, key *storekey.Key) *blobcache.Cache {
	dir := c.GlobalString("cache-dir")
	if dir == "" {
		return nil
	}

	limit, err := humanize.ParseBytes(c.GlobalString("blob-cache-size"))
	if err != nil {
		Fatalf("invalid --blob-cache-size: %v", err)
	}

	storeID := storageID(c)
	if key != nil {
		storeID = key.IDString()
	}

	cache, err := blobcache.Open(filepath.Join(dir, "blobs"), storeID, int64(limit))
	if err != nil {
		Fatalf("opening blob cache: %v", err)
	}

	return cache
}

// SweepBlobCache trims the cache to its size limit and logs the result.
func SweepBlobCache(cache *blobcache.Cache) {
	if cache == nil {
		return
	}

	removed, err := cache.Sweep()
	if err != nil {
		log.Printf("sweeping blob cache: %v", err)
	}

	if removed > 0 {
		log.Printf("Evicted %d blobs from the cache", removed)
	}
}
