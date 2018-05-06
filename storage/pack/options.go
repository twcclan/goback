package pack

type packOptions struct {
	compaction  bool
	maxParallel uint
}

type packOption func(p *packOptions)

func WithCompaction(enabled bool) packOption {
	return func(p *packOptions) {
		p.compaction = enabled
	}
}

func WithMaxParallel(max uint) packOption {
	return func(p *packOptions) {
		p.maxParallel = max
	}
}
