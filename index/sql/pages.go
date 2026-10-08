package sql

import (
	"context"
	"errors"
	"time"

	"github.com/gobackio/goback/backup"
	"github.com/gobackio/goback/index"
	"github.com/gobackio/goback/index/sql/ent"
	"github.com/gobackio/goback/index/sql/ent/commitrow"
	"github.com/gobackio/goback/index/sql/ent/file"
	"github.com/gobackio/goback/index/sql/ent/predicate"
	"github.com/gobackio/goback/index/sql/ent/tree"
	"github.com/gobackio/goback/index/sql/mapping"
	"github.com/gobackio/goback/proto"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

// CommitDetailsBefore pages through a set's live, complete commits by
// receipt, newest first: those received before before, or all of them
// when it is zero. It returns at most limit, every one when limit is
// zero, and then the others received at the same instant as the last, so
// the last one's ReceivedAtNs is the before of the next page.
func (x *Index) CommitDetailsBefore(ctx context.Context, backupSet string, before time.Time, limit int) ([]index.CommitDetail, error) {
	setID, err := findSet(ctx, x.client, backupSet)
	if errors.Is(err, backup.ErrNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	live := func() *ent.CommitRowQuery {
		return x.client.CommitRow.Query().Where(commitrow.SetID(setID), commitrow.Partial(false), liveCommit()).WithSet()
	}

	query := live().Order(ent.Desc(commitrow.FieldReceivedAt), ent.Desc(commitrow.FieldID))
	if !before.IsZero() {
		query.Where(commitrow.ReceivedAtLT(before.UTC()))
	}

	if limit > 0 {
		query.Limit(limit)
	}

	rows, err := query.All(ctx)
	if err != nil {
		return nil, err
	}

	if limit > 0 && len(rows) == limit {
		last := rows[len(rows)-1]

		rest, err := live().Where(commitrow.ReceivedAt(last.ReceivedAt), commitrow.IDLT(last.ID)).
			Order(ent.Desc(commitrow.FieldID)).All(ctx)
		if err != nil {
			return nil, err
		}

		rows = append(rows, rest...)
	}

	return mapAll(rows, m.CommitDetail), nil
}

// TrashedCommits implements backup.Retention.
func (x *Index) TrashedCommits(ctx context.Context, backupSet string, before time.Time, limit int) ([]*proto.TrashedCommit, error) {
	setID, err := findSet(ctx, x.client, backupSet)
	if errors.Is(err, backup.ErrNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	trashed := func() *ent.CommitRowQuery {
		return x.client.CommitRow.Query().
			Where(commitrow.SetID(setID), commitrow.Partial(false), commitrow.DeletedAtNotNil(), commitrow.TombstonedAtIsNil()).
			WithSet()
	}

	query := trashed().Order(ent.Desc(commitrow.FieldDeletedAt), ent.Desc(commitrow.FieldID))
	if !before.IsZero() {
		query.Where(commitrow.DeletedAtLT(before.UTC()))
	}

	if limit > 0 {
		query.Limit(limit)
	}

	rows, err := query.All(ctx)
	if err != nil {
		return nil, err
	}

	if limit > 0 && len(rows) == limit {
		last := rows[len(rows)-1]

		rest, err := trashed().Where(commitrow.DeletedAt(*last.DeletedAt), commitrow.IDLT(last.ID)).
			Order(ent.Desc(commitrow.FieldID)).All(ctx)
		if err != nil {
			return nil, err
		}

		rows = append(rows, rest...)
	}

	return mapAll(rows, m.TrashedCommit), nil
}

// VersionsBefore pages through the versions of a path that a live commit
// of the set contains, newest first: those first held before before, or
// all of them when it is zero, at most limit, every one when limit is
// zero. The last one's From is the before of the next page.
func (x *Index) VersionsBefore(ctx context.Context, backupSet string, name string, before time.Time, limit int) ([]index.Version, error) {
	setID, err := findSet(ctx, x.client, backupSet)
	if errors.Is(err, backup.ErrNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	query := x.client.File.Query().
		Where(file.SetID(setID), file.Path(name), predicate.File(heldByCommit(liveCommitColumns))).
		Order(ent.Desc(file.FieldValidFrom))
	if !before.IsZero() {
		query.Where(file.ValidFromLT(before.UTC()))
	}

	if limit > 0 {
		query.Limit(limit)
	}

	rows, err := query.All(ctx)
	if err != nil {
		return nil, err
	}

	return mapAll(rows, m.Version), nil
}

// ReadDirAfter pages through what ReadDir lists, ordered by path byte by
// byte rather than by name: the entries whose paths sort after after, at
// most limit, every one when limit is zero. An entry's path is
// proto.JoinPath(dir, its name), and the last one's is the after of the
// next page.
func (x *Index) ReadDirAfter(ctx context.Context, backupSet string, dir string, notAfter time.Time, after string, limit int) ([]*proto.TreeNode, error) {
	setID, err := findSet(ctx, x.client, backupSet)
	if errors.Is(err, backup.ErrNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	at := notAfter.UTC()

	files := x.client.File.Query().Where(
		file.SetID(setID), file.Dir(dir), file.ValidFromLTE(at),
		file.Or(file.ValidUntilIsNil(), file.ValidUntilGT(at)),
		predicate.File(heldByCommit(liveCommitColumns)), predicate.File(pathAfter(after)),
	).Order(byPath)

	trees := x.client.Tree.Query().Where(
		tree.SetID(setID), tree.Dir(dir), tree.ValidFromLTE(at),
		tree.Or(tree.ValidUntilIsNil(), tree.ValidUntilGT(at)),
		predicate.Tree(heldByCommit(liveCommitColumns)), predicate.Tree(pathAfter(after)),
	).Order(byPath)

	if limit > 0 {
		files.Limit(limit)
		trees.Limit(limit)
	}

	fileRows, err := files.All(ctx)
	if err != nil {
		return nil, err
	}

	treeRows, err := trees.All(ctx)
	if err != nil {
		return nil, err
	}

	var entries []*proto.TreeNode
	for len(fileRows) > 0 || len(treeRows) > 0 {
		if limit > 0 && len(entries) == limit {
			break
		}

		if len(treeRows) == 0 || (len(fileRows) > 0 && fileRows[0].Path < treeRows[0].Path) {
			entries = append(entries, m.TreeNode(fileRows[0]))
			fileRows = fileRows[1:]

			continue
		}

		entries = append(entries, directory(treeRows[0]))
		treeRows = treeRows[1:]
	}

	return entries, nil
}

// pathAfter is the condition that a files or trees row's path sorts after
// after, byte by byte.
func pathAfter(after string) func(*entsql.Selector) {
	return func(s *entsql.Selector) {
		column := bytewise(s, s.C(file.FieldPath))
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.WriteString(column).WriteOp(entsql.OpGT).Arg(after)
		}))
	}
}

// byPath orders files or trees rows by path, byte by byte.
func byPath(s *entsql.Selector) {
	s.OrderExpr(entsql.Expr(bytewise(s, s.C(file.FieldPath))))
}

// bytewise is a text column as compared byte by byte, which SQLite does
// by default and Postgres only under the C collation.
func bytewise(s *entsql.Selector, column string) string {
	if s.Dialect() == dialect.Postgres {
		return column + ` COLLATE "C"`
	}

	return column
}

// directory is a trees row as ReadDir lists it.
func directory(row *ent.Tree) *proto.TreeNode {
	return &proto.TreeNode{
		Stat: &proto.FileInfo{Name: mapping.Base(row.Path), Type: proto.NodeType_NODE_DIRECTORY},
		Ref:  mapping.Ref(row.Ref),
	}
}
