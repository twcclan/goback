package pack

import (
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
	atRest          *AtRestKey
}

// WithAtRestKey seals every payload written from now on under the key
// and opens the ones sealed under it; records written without a key stay
// readable. A key file made by goback key new serves.
func WithAtRestKey(key *storekey.Key) PackOption {
	return func(p *packOptions) {
		p.atRest = NewAtRestKey(key)
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

type PackOption func(p *packOptions)

// CompactionConfig tunes Compact; when it runs is the caller's business.
type CompactionConfig struct {
	// MinimumCandidates is how many eligible archives a placement group
	// needs before Compact rewrites it.
	MinimumCandidates int
}

func WithCompaction(config CompactionConfig) PackOption {
	return func(p *packOptions) {
		p.compaction = config
	}
}

func WithMaxParallel(max uint) PackOption {
	return func(p *packOptions) {
		p.maxParallel = max
	}
}

func WithMaxSize(max uint64) PackOption {
	return func(p *packOptions) {
		p.maxSize = max
	}
}

func WithArchiveStorage(storage ArchiveStorage) PackOption {
	return func(p *packOptions) {
		p.storage = storage
	}
}

func WithCloseBeforeRead(do bool) PackOption {
	return func(p *packOptions) {
		p.closeBeforeRead = do
	}
}

func WithMetadataCache(cache backup.ObjectStore) PackOption {
	return func(p *packOptions) {
		p.cache = cache
	}
}

func WithArchiveIndex(index ArchiveIndex) PackOption {
	return func(p *packOptions) {
		p.index = index
	}
}
