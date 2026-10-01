package sql

import (
	"context"
	"errors"
	"fmt"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/index/sql/ent"
	"github.com/twcclan/goback/index/sql/ent/commitrow"
	"github.com/twcclan/goback/index/sql/ent/setref"
	"github.com/twcclan/goback/proto"
)

// pruneRefs drops the set's set_refs rows that only the tombstoned
// commits reach, so their trees and files stop being readable before the
// collector erases them. A ref a live commit shares stays. The live
// commits are walked without the set lock, and the ones indexed meanwhile
// under it, so a concurrent commit cannot lose a ref.
func (x *Index) pruneRefs(ctx context.Context, setID int64, tombstoned [][]byte) error {
	dead := map[string]bool{}
	reached := map[string]bool{}

	for _, ref := range tombstoned {
		dead[string(ref)] = true

		obj, err := x.ObjectStore.Get(ctx, &proto.Ref{Hash: ref})
		if errors.Is(err, backup.ErrNotFound) {
			continue
		}

		if err != nil {
			return err
		}

		commit := obj.GetCommit()
		if commit == nil {
			continue
		}

		err = x.walkRefs(ctx, commit.GetTree(), reached, func(hash []byte) { dead[string(hash)] = true })
		if err != nil && !errors.Is(err, backup.ErrNotFound) {
			return err
		}
	}

	visited := map[string]bool{}
	walked := map[string]bool{}

	subtractLive := func(c *ent.Client) error {
		commits, err := c.CommitRow.Query().Where(commitrow.SetID(setID), commitrow.TombstonedAtIsNil()).All(ctx)
		if err != nil {
			return err
		}

		for _, commit := range commits {
			if walked[string(commit.Ref)] {
				continue
			}

			walked[string(commit.Ref)] = true
			delete(dead, string(commit.Ref))

			err := x.walkRefs(ctx, &proto.Ref{Hash: commit.Tree}, visited, func(hash []byte) { delete(dead, string(hash)) })
			if err != nil {
				return fmt.Errorf("walking live commit %x: %w", commit.Ref, err)
			}
		}

		return nil
	}

	if err := subtractLive(x.client); err != nil {
		return err
	}

	return x.tx(ctx, func(tx *ent.Tx) error {
		if _, err := x.lockSet(ctx, tx, setID); err != nil {
			return err
		}

		if err := subtractLive(tx.Client()); err != nil {
			return err
		}

		refs := make([][]byte, 0, len(dead))
		for ref := range dead {
			refs = append(refs, []byte(ref))
		}

		for start := 0; start < len(refs); start += objectBatch {
			chunk := refs[start:min(start+objectBatch, len(refs))]

			_, err := tx.SetRef.Delete().Where(setref.SetID(setID), setref.RefIn(chunk...)).Exec(ctx)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// walkRefs visits the refs a tree reaches that set_refs records: the
// tree and its splits, every file, and the splits of a large file.
func (x *Index) walkRefs(ctx context.Context, tree *proto.Ref, visited map[string]bool, visit func([]byte)) error {
	if visited[string(tree.GetHash())] {
		return nil
	}

	visited[string(tree.GetHash())] = true
	visit(tree.GetHash())

	obj, err := x.ObjectStore.Get(ctx, tree)
	if err != nil {
		return err
	}

	t := obj.GetTree()
	if t == nil {
		return fmt.Errorf("object %x is not a tree", tree.GetHash())
	}

	for _, split := range t.GetSplits() {
		if err := x.walkRefs(ctx, split, visited, visit); err != nil {
			return err
		}
	}

	for _, node := range t.GetNodes() {
		if node.GetRef() == nil {
			continue
		}

		switch node.GetStat().GetType() {
		case proto.NodeType_NODE_DIRECTORY:
			if err := x.walkRefs(ctx, node.GetRef(), visited, visit); err != nil {
				return err
			}
		case proto.NodeType_NODE_FILE:
			visit(node.GetRef().GetHash())

			if node.GetStat().GetSize() < backup.SplitFileSize {
				continue
			}

			obj, err := x.ObjectStore.Get(ctx, node.GetRef())
			if err != nil {
				return err
			}

			for _, split := range obj.GetFile().GetSplits() {
				visit(split.GetHash())
			}
		}
	}

	return nil
}
