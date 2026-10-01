package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/twcclan/goback/backup"
)

// WALBackup commits the spool to the WAL set. Every commit holds each WAL
// file back to the oldest base backup inside Window, so any one commit
// replays on its own.
type WALBackup struct {
	// Walker stores the commit; its Root is the spool's directory and its
	// Set the WAL set. Run sets its Include, Carry and Metadata.
	Walker *backup.Walker
	Spool  Spool
	// BaseSet is the set the cluster's base backups go to.
	BaseSet string
	Window  time.Duration
	Now     func() time.Time
}

// maxBases bounds the base backups read to find the cutoff.
const maxBases = 1000

// Run commits what the spool holds and then deletes it from the spool. It
// commits nothing, and returns nil, when the spool is empty. It refuses WAL
// from another cluster than the set's and WAL that skips a segment, and
// then leaves the spool as it was.
func (b *WALBackup) Run(ctx context.Context) (*backup.WalkResult, error) {
	spooled, err := b.Spool.Files()
	if err != nil {
		return nil, err
	}

	if len(spooled) == 0 {
		return nil, nil
	}

	w := b.Walker

	held, systemID, err := b.previous(ctx)
	if err != nil {
		return nil, err
	}

	bases, err := w.Index.CommitInfo(ctx, b.BaseSet, b.Now(), maxBases)
	if err != nil {
		return nil, fmt.Errorf("listing the base backups: %w", err)
	}

	carry := func(string) bool { return true }
	if cutoff, ok := Cutoff(bases, b.Window, b.Now()); ok {
		carry = Carry(cutoff)
	}

	var segSize uint64
	for _, name := range spooled {
		if !isSegment(name) {
			continue
		}

		id, size, err := b.checkSpooled(name, systemID)
		if err != nil {
			return nil, err
		}

		systemID, segSize = id, size
	}

	names := slices.Clone(spooled)
	for _, name := range held {
		if carry(name) && !slices.Contains(spooled, name) {
			names = append(names, name)
		}
	}

	slices.Sort(names)

	if segSize != 0 {
		if err := checkContiguous(names, segSize); err != nil {
			return nil, err
		}
	}

	w.Include = func(rel string) bool { return slices.Contains(spooled, rel) }
	w.Carry = carry
	w.Metadata = map[string]string{
		MetaFirstWALFile: firstSegment(names),
		MetaLastWALFile:  lastSegment(names),
	}

	if systemID != 0 {
		w.Metadata[MetaSystemID] = strconv.FormatUint(systemID, 10)
	}

	result, err := w.Run(ctx)
	if err != nil {
		return nil, err
	}

	return result, b.Spool.Remove(spooled...)
}

// previous lists the WAL files the set's latest commit holds, and the
// cluster it belongs to; a set without commits holds none.
func (b *WALBackup) previous(ctx context.Context) ([]string, uint64, error) {
	w := b.Walker

	ref, err := w.Index.LatestCommit(ctx, w.Set)
	if errors.Is(err, backup.ErrNotFound) {
		return nil, 0, nil
	}

	if err != nil {
		return nil, 0, err
	}

	obj, err := w.Objects.Get(ctx, ref)
	if err != nil {
		return nil, 0, err
	}

	commit := obj.GetCommit()

	var systemID uint64
	if id, ok := commit.GetMetadata()[MetaSystemID]; ok {
		if systemID, err = strconv.ParseUint(id, 10, 64); err != nil {
			return nil, 0, fmt.Errorf("the WAL set's system id %q: %w", id, err)
		}
	}

	tree, err := backup.OpenTree(ctx, w.Objects, commit.GetTree(), w.Key, nil)
	if err != nil {
		return nil, 0, err
	}

	names := make([]string, 0, len(tree.Nodes))
	for _, node := range tree.Nodes {
		names = append(names, string(node.Stat.Name))
	}

	return names, systemID, nil
}

func (b *WALBackup) checkSpooled(name string, systemID uint64) (uint64, uint64, error) {
	file, err := b.Spool.Open(name)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	hdr, err := readSegmentHeader(file)
	if err != nil {
		return 0, 0, fmt.Errorf("%s: %w", name, err)
	}

	if _, err := file.Seek(0, 0); err != nil {
		return 0, 0, err
	}

	id, err := checkSegment(name, file, systemID)

	return id, uint64(hdr.segSize), err
}

func firstSegment(names []string) string {
	for _, name := range names {
		if isSegment(name) {
			return name
		}
	}

	return ""
}

func lastSegment(names []string) string {
	for i := len(names) - 1; i >= 0; i-- {
		if isSegment(names[i]) {
			return names[i]
		}
	}

	return ""
}
