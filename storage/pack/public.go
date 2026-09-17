package pack

import (
	"slices"
	"strings"
)

// WithPublicStorage keeps the archives of the public prefix in their own
// storage instead of under the root, so several stores can share one.
// Names keep their public/ prefix in the index; the root's own public/
// entries are not listed. DeleteAll leaves the public storage alone.
func WithPublicStorage(public ArchiveStorage) PackOption {
	return func(p *packOptions) {
		p.public = public
	}
}

const publicPrefix = placementPublic + "/"

// routedStorage sends public/ names to the public storage and everything
// else to the root.
type routedStorage struct {
	root   ArchiveStorage
	public ArchiveStorage
}

func (r *routedStorage) route(name string) (ArchiveStorage, string) {
	if rest, ok := strings.CutPrefix(name, publicPrefix); ok {
		return r.public, rest
	}

	return r.root, name
}

func (r *routedStorage) Create(name string) (File, error) {
	storage, name := r.route(name)

	return storage.Create(name)
}

func (r *routedStorage) Open(name string) (File, error) {
	storage, name := r.route(name)

	return storage.Open(name)
}

func (r *routedStorage) Delete(name string) error {
	storage, name := r.route(name)

	return storage.Delete(name)
}

func (r *routedStorage) List(extension string) ([]string, error) {
	names, err := r.root.List(extension)
	if err != nil {
		return nil, err
	}

	names = slices.DeleteFunc(names, func(name string) bool { return strings.HasPrefix(name, publicPrefix) })

	public, err := r.public.List(extension)
	if err != nil {
		return nil, err
	}

	for _, name := range public {
		names = append(names, publicPrefix+name)
	}

	return names, nil
}

func (r *routedStorage) DeleteAll() error {
	return r.root.DeleteAll()
}

// WalkPublicRefs calls fn with every ref the store uploaded to the public
// prefix, in no particular order; a job collecting a shared public
// storage unions these over every store.
func (ps *PackStorage) WalkPublicRefs(fn func(ref []byte) error) error {
	return ps.index.WalkPublicRefs(fn)
}

// Compact rewrites the archives compaction picks and returns when it is
// done; a store server runs it as a job.
func (ps *PackStorage) Compact() error {
	return ps.doCompaction()
}
