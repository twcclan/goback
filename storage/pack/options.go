package pack

import (
	"log/slog"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/storekey"
)

type packOptions struct {
	compaction      CompactionConfig
	maxParallel     uint
	maxSize         uint64
	closeBeforeRead bool
	storage         ArchiveStorage
	index           ArchiveIndex
	cache           backup.ObjectStore
	idleFinalize    time.Duration
	sessionLease    time.Duration
	atRestKey       *storekey.Key
	atRestRetired   []*storekey.Key
	logger          *slog.Logger
}

// WithAtRestKey seals every payload written from now on under the key
// and opens the ones sealed under it; records written without a key stay
// readable. A key file made by goback key new serves.
func WithAtRestKey(key *storekey.Key) PackOption {
	return func(p *packOptions) {
		p.atRestKey = key
	}
}

// WithRetiredAtRestKey keeps a key records may still be sealed under. It
// is only read with, and a rewrite re-seals what it opens under the
// current key, so the retired key can go once no record names it.
func WithRetiredAtRestKey(key *storekey.Key) PackOption {
	return func(p *packOptions) {
		p.atRestRetired = append(p.atRestRetired, key)
	}
}

// WithLogger is where the store reports; the default is slog.Default.
func WithLogger(logger *slog.Logger) PackOption {
	return func(p *packOptions) {
		p.logger = logger
	}
}

// WithIdleFinalize finalizes a session's open archive after it has not
// been written to for the given duration.
func WithIdleFinalize(d time.Duration) PackOption {
	return func(p *packOptions) {
		p.idleFinalize = d
	}
}

// WithSessionLease ends sessions that have not written for the given
// duration, dropping what they did not commit.
func WithSessionLease(d time.Duration) PackOption {
	return func(p *packOptions) {
		p.sessionLease = d
	}
}

// PackOption configures NewPackStorage.
type PackOption func(p *packOptions)

// CompactionConfig tunes Compact; when it runs is the caller's business.
type CompactionConfig struct {
	// MinimumCandidates is how many eligible archives Compact needs before
	// it rewrites them.
	MinimumCandidates int
}

// WithCompaction tunes Compact.
func WithCompaction(config CompactionConfig) PackOption {
	return func(p *packOptions) {
		p.compaction = config
	}
}

// WithMaxParallel bounds the archives open for writing at once.
func WithMaxParallel(max uint) PackOption {
	return func(p *packOptions) {
		p.maxParallel = max
	}
}

// WithMaxSize is the size at which an open archive is finalized.
func WithMaxSize(max uint64) PackOption {
	return func(p *packOptions) {
		p.maxSize = max
	}
}

// WithArchiveStorage is where the archives live; required.
func WithArchiveStorage(storage ArchiveStorage) PackOption {
	return func(p *packOptions) {
		p.storage = storage
	}
}

// WithCloseBeforeRead finalizes an open archive before an object is read
// from it, for storages that cannot read an upload in flight.
func WithCloseBeforeRead(do bool) PackOption {
	return func(p *packOptions) {
		p.closeBeforeRead = do
	}
}

// WithMetadataCache keeps commits, trees and files in cache as well.
func WithMetadataCache(cache backup.ObjectStore) PackOption {
	return func(p *packOptions) {
		p.cache = cache
	}
}

// WithArchiveIndex is the index of archives and sessions; required.
func WithArchiveIndex(index ArchiveIndex) PackOption {
	return func(p *packOptions) {
		p.index = index
	}
}
