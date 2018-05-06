package pack

type packOptions struct {
	compaction      bool
	maxParallel     uint
	maxSize         uint64
	closeBeforeRead bool
	storage         ArchiveStorage
}

type PackOption func(p *packOptions)

func WithCompaction(enabled bool) PackOption {
	return func(p *packOptions) {
		p.compaction = enabled
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
