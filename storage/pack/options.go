package pack

import (
	"time"

	"github.com/twcclan/goback/backup"
)

type packOptions struct {
	compaction      CompactionConfig
	maxParallel     uint
	maxSize         uint64
	closeBeforeRead bool
	storage         ArchiveStorage
	index           ArchiveIndex
	cache           backup.ObjectStore
}

type PackOption func(p *packOptions)

type CompactionConfig struct {
	// AfterFlush controls whether background compaction is triggered
	// after a Flush happens on the store
	AfterFlush bool

	// OnClose controls whether a compaction is done when closing the
	// PackStore
	OnClose bool

	// OnOpen controls whether a compaction is done after opening the
	// PackStore
	OnOpen bool

	// If Periodically is set to a non zero value the store will spawn
	// a goroutine that will periodically run a compaction
	Periodically time.Duration

	// MinimumCandidates specifies the minimum number of eligible archives
	// that need to exist before a compaction happens
	MinimumCandidates int

	// GarbageCollection will enable the garbage collector on this store.
	// Before running a compaction, it will do a full reachability check
	// on all objects stored to figure out which can be removed, because
	// they are not referenced by a commit anymore
	GarbageCollection bool
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
