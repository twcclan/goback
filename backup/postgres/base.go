package postgres

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/twcclan/goback/backup"
)

// BaseTar is the file a base backup's commit holds: the tar pg_basebackup
// writes with -D - -Ft.
const BaseTar = "base.tar"

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
// set's next commit, together with what the backup records about itself.
// wait, when set, is called once tar is read to its end and reports how its
// writer ended; an error from it leaves the set without a commit.
func (b *BaseBackup) Run(ctx context.Context, tar io.Reader, wait func() error) (*backup.WalkResult, error) {
	w := b.Walker

	systemID, err := b.previous(ctx)
	if err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()
	parsed := make(chan parseResult, 1)

	go func() {
		info, err := parseBase(pr)
		// the tee writes everything it reads into the pipe, so the parse has
		// to drain it even once it has given up
		_, _ = io.Copy(io.Discard, pr)
		parsed <- parseResult{info, err}
	}()

	w.Stream = &backup.Stream{
		Name:    BaseTar,
		Content: io.TeeReader(tar, pw),
		Finish: func() error {
			_ = pw.Close()
			result := <-parsed

			if wait != nil {
				if err := wait(); err != nil {
					return err
				}
			}

			if result.err != nil {
				return result.err
			}

			if systemID != 0 && result.info.SystemID != systemID {
				return fmt.Errorf("%w: the backup is of %d, the set of %d", ErrForeignCluster, result.info.SystemID, systemID)
			}

			w.Metadata = result.info.Metadata()

			return nil
		},
	}

	result, err := w.Run(ctx)
	_ = pw.CloseWithError(errors.New("the base backup ended"))

	return result, err
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

type parseResult struct {
	info BaseInfo
	err  error
}

// parseBase reads what a base backup's tar records about it.
func parseBase(r io.Reader) (BaseInfo, error) {
	var (
		info                         BaseInfo
		label, manifest, controlFile bool
	)

	entries := tar.NewReader(r)
	for {
		hdr, err := entries.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return info, fmt.Errorf("reading the base backup: %w", err)
		}

		switch hdr.Name {
		case "backup_label":
			err, label = info.readBackupLabel(entries), true
		case "backup_manifest":
			err, manifest = info.readManifest(entries), true
		case "global/pg_control":
			err, controlFile = info.readControl(entries), true
		}

		if err != nil {
			return info, err
		}
	}

	switch {
	case !label:
		return info, errors.New("the base backup holds no backup_label")
	case !manifest:
		return info, errors.New("the base backup holds no backup_manifest")
	case !controlFile:
		return info, errors.New("the base backup holds no global/pg_control")
	}

	return info, nil
}
