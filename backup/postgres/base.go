package postgres

import (
	archive "archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/twcclan/goback/backup"
)

// ErrForeignCluster is returned for a base backup of another cluster than
// the set's.
var ErrForeignCluster = errors.New("base backup of another cluster")

// BaseBackup commits one base backup to the base set.
type BaseBackup struct {
	// Walker stores the commit; its Set is the base set. Run sets its
	// Stream and Metadata.
	Walker *backup.Walker
}

// Run stores tar, the output of pg_basebackup -D - -Ft -X fetch, as the
// set's next commit: the data directory as a tree, together with what the
// backup records about itself. wait, when set, is called once tar is read to
// its end and reports how its writer ended; an error from it leaves the set
// without a commit.
func (b *BaseBackup) Run(ctx context.Context, tar io.Reader, wait func() error) (*backup.WalkResult, error) {
	w := b.Walker

	systemID, err := b.previous(ctx)
	if err != nil {
		return nil, err
	}

	described := map[string]*bytes.Buffer{
		"backup_label":      nil,
		"backup_manifest":   nil,
		"global/pg_control": nil,
	}

	w.Stream = &backup.Stream{
		Tar: tar,
		Inspect: func(hdr *archive.Header) io.Writer {
			if _, ok := described[hdr.Name]; !ok {
				return nil
			}

			described[hdr.Name] = &bytes.Buffer{}

			return described[hdr.Name]
		},
		Finish: func() error {
			if wait != nil {
				if err := wait(); err != nil {
					return err
				}
			}

			info, err := describe(described)
			if err != nil {
				return err
			}

			if systemID != 0 && info.SystemID != systemID {
				return fmt.Errorf("%w: the backup is of %d, the set of %d", ErrForeignCluster, info.SystemID, systemID)
			}

			w.Metadata = info.Metadata()

			return nil
		},
	}

	return w.Run(ctx)
}

// previous names the cluster the set's latest commit belongs to; zero for a
// set without one.
func (b *BaseBackup) previous(ctx context.Context) (uint64, error) {
	w := b.Walker

	ref, err := w.Index.LatestCommit(ctx, w.Set)
	if errors.Is(err, backup.ErrNotFound) {
		return 0, nil
	}

	if err != nil {
		return 0, err
	}

	obj, err := w.Objects.Get(ctx, ref)
	if err != nil {
		return 0, err
	}

	id, ok := obj.GetCommit().GetMetadata()[MetaSystemID]
	if !ok {
		return 0, nil
	}

	systemID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("the base set's system id %q: %w", id, err)
	}

	return systemID, nil
}

// describe reads what a base backup records about itself out of the files
// that hold it.
func describe(files map[string]*bytes.Buffer) (BaseInfo, error) {
	var info BaseInfo

	for name, content := range files {
		if content == nil {
			return info, fmt.Errorf("the base backup holds no %s", name)
		}
	}

	if err := info.readBackupLabel(files["backup_label"]); err != nil {
		return info, err
	}

	if err := info.readManifest(files["backup_manifest"]); err != nil {
		return info, err
	}

	return info, info.readControl(files["global/pg_control"])
}
