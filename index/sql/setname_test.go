package sql

import (
	"testing"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

func TestASetCannotBeNamedLikeAnID(t *testing.T) {
	f := newFixture(t)

	_, err := f.x.BeginCommit(f.ctx, "2026")
	require.ErrorIs(t, err, backup.ErrSetName)

	root := f.tree(f.file("a", "a"))
	err = f.x.Put(f.ctx, proto.NewObject(&proto.Commit{Timestamp: f.clock.Unix(), Tree: root.Ref(), BackupSet: "2026"}))
	require.ErrorIs(t, err, backup.ErrSetName)

	_, err = f.x.BeginCommit(f.ctx, "2026-backups")
	require.NoError(t, err)
}

func TestRebuildKeepsASetNamedLikeAnID(t *testing.T) {
	f := newFixture(t)

	root := f.tree(f.file("a", "a"))
	require.NoError(t, f.store.Put(f.ctx, proto.NewObject(&proto.Commit{Timestamp: f.clock.Unix(), Tree: root.Ref(), BackupSet: "2026", SetId: 9})))

	y := f.index()
	require.NoError(t, y.ReIndex(f.ctx))
	require.Equal(t, "active", f.setState(y, "2026"))
}
