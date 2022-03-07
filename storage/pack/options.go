package pack

import (
	"errors"
	"time"
)

var defaultOptions = Options{
	MaxParallel: 1,
	MaxSize:     1024 * 1024 * 1024,
	Compaction: CompactionConfig{
		MinimumCandidates: 1000,
	},
}

func GetOptions(options ...Option) *Options {
	return CopyOptions(defaultOptions, options...)
}

func CopyOptions(original Options, with ...Option) *Options {
	for _, opt := range with {
		opt(&original)
	}

	return &original
}

func validateOptions(opts *Options) (*Options, error) {
	if opts.Storage == nil {
		return nil, errors.New("no archive storage provided")
	}

	if opts.Index == nil {
		return nil, errors.New("no archive locator provided")
	}

	return opts, nil
}

type Options struct {
	// Storage is the backing storage for the archive files
	Storage ArchiveStorage

	// Index is used to index and lookup archive contents
	Index ArchiveIndex

	// Compaction defines when and how compaction should take place
	Compaction CompactionConfig

	// MaxParallel is the maximum number of archives that will be written to in parallel
	MaxParallel uint

	// MaxSize is the maximum number of bytes that will be written to an archive file
	MaxSize uint64
}

type Option func(p *Options)

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

func WithCompaction(config CompactionConfig) Option {
	return func(p *Options) {
		p.Compaction = config
	}
}

func WithMaxParallel(max uint) Option {
	return func(p *Options) {
		p.MaxParallel = max
	}
}

func WithMaxSize(max uint64) Option {
	return func(p *Options) {
		p.MaxSize = max
	}
}

func WithArchiveStorage(storage ArchiveStorage) Option {
	return func(p *Options) {
		p.Storage = storage
	}
}

func WithArchiveIndex(index ArchiveIndex) Option {
	return func(p *Options) {
		p.Index = index
	}
}
