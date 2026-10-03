package common

import (
	"crypto/sha256"
	"fmt"
	"log"

	"github.com/twcclan/goback/backup/blobcache"
	"github.com/twcclan/goback/backup/storekey"

	"github.com/dustin/go-humanize"
	"github.com/urfave/cli"
)

// BlobCache opens the blob cache named by the global --blob-cache flag for
// the store the command works against, or returns nil when there is none.
func BlobCache(c *cli.Context, key *storekey.Key) *blobcache.Cache {
	dir := c.GlobalString("blob-cache")
	if dir == "" {
		return nil
	}

	limit, err := humanize.ParseBytes(c.GlobalString("blob-cache-size"))
	if err != nil {
		Fatalf("invalid --blob-cache-size: %v", err)
	}

	storeID := ""
	if key != nil {
		storeID = key.IDString()
	} else {
		sum := sha256.Sum256([]byte(c.GlobalString("storage")))
		storeID = fmt.Sprintf("%x", sum[:8])
	}

	cache, err := blobcache.Open(dir, storeID, int64(limit))
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
