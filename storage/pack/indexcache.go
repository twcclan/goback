package pack

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gobackio/goback/proto"
)

// WithIndexCache keeps a copy of every archive index the store reads under
// dir, so reading it again costs no storage request. A copy keeps the
// original's creation time, which versions the archive's records.
func WithIndexCache(dir string) PackOption {
	return func(p *packOptions) {
		p.indexCache = dir
	}
}

// indexCache serves index files from local copies; an index file never
// changes once written.
type indexCache struct {
	ArchiveStorage
	dir string
}

var (
	_ RangeSigner  = (*indexCache)(nil)
	_ Checksummer  = (*indexCache)(nil)
	_ InfoLister   = (*indexCache)(nil)
	_ ListedOpener = (*indexCache)(nil)
)

func (c *indexCache) path(name string) string {
	return filepath.Join(c.dir, filepath.FromSlash(name))
}

func (c *indexCache) Open(name string) (File, error) {
	if !strings.HasSuffix(name, IndexExt) {
		return c.ArchiveStorage.Open(name)
	}

	if file, err := os.Open(c.path(name)); err == nil {
		return file, nil
	}

	src, err := c.ArchiveStorage.Open(name)
	if err != nil {
		return nil, err
	}

	created, err := c.keep(name, src)
	if err != nil {
		return nil, err
	}

	if !created {
		return c.ArchiveStorage.Open(name)
	}

	return os.Open(c.path(name))
}

// keep copies src under the cache and closes it. It reports false when
// the file cannot be kept, as one still being written cannot.
func (c *indexCache) keep(name string, src File) (bool, error) {
	defer src.Close()

	info, err := src.Stat()
	if err != nil || info.ModTime().IsZero() {
		return false, nil
	}

	path := c.path(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())

	copied, err := io.Copy(tmp, src)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}

	if err != nil {
		return false, fmt.Errorf("caching index %s: %w", name, err)
	}

	if copied != info.Size() {
		return false, fmt.Errorf("caching index %s: read %d of %d bytes", name, copied, info.Size())
	}

	modified := info.ModTime().Truncate(time.Microsecond)
	if err := os.Chtimes(tmp.Name(), modified, modified); err != nil {
		return false, err
	}

	return true, os.Rename(tmp.Name(), path)
}

func (c *indexCache) Delete(name string) error {
	err := c.ArchiveStorage.Delete(name)

	if strings.HasSuffix(name, IndexExt) {
		_ = os.Remove(c.path(name))
	}

	return err
}

// SignRange implements RangeSigner when the storage underneath does.
func (c *indexCache) SignRange(ctx context.Context, name string, offset, length int64, ttl time.Duration) (*proto.Location, error) {
	signer, ok := c.ArchiveStorage.(RangeSigner)
	if !ok {
		return nil, ErrNoSignedURL
	}

	return signer.SignRange(ctx, name, offset, length, ttl)
}

// Checksum implements Checksummer when the storage underneath does.
func (c *indexCache) Checksum(name string) ([]byte, error) {
	sums, ok := c.ArchiveStorage.(Checksummer)
	if !ok {
		return nil, nil
	}

	return sums.Checksum(name)
}

// OpenListed implements ListedOpener: an index comes from the cache.
func (c *indexCache) OpenListed(file ListedFile) (File, error) {
	if strings.HasSuffix(file.Name, IndexExt) {
		return c.Open(file.Name)
	}

	return OpenListed(c.ArchiveStorage, file)
}

// ListInfo implements InfoLister over the storage underneath.
func (c *indexCache) ListInfo(extension string) ([]ListedFile, error) {
	return ListInfo(c.ArchiveStorage, extension)
}
