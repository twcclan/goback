package postgres

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

func (f *walFixture) restore(at time.Time) (*RestoreResult, string, error) {
	f.t.Helper()

	dir := filepath.Join(f.t.TempDir(), "data")
	r := &Restore{
		Objects: f.index,
		Latest:  func(ctx context.Context, set string) (*proto.Ref, error) { return f.index.LatestCommit(ctx, set) },
		BaseSet: "db-base",
		WALSet:  "db-wal",
		At:      at,
		RestoreCommand: func(wal *proto.Ref) string {
			return "goback postgres wal-get --commit " + hex.EncodeToString(wal.Hash) + " %f %p"
		},
	}

	result, err := r.Run(f.ctx, dir)

	return result, dir, err
}

func TestARestoreWritesTheNewestBaseAndRecoversFromTheWALSet(t *testing.T) {
	f := newWALFixture(t)

	content := baseTar(t, 42)
	_, err := f.runBase(content, nil)
	require.NoError(t, err)

	f.archive(7, 42)
	f.archive(8, 42)
	wal, err := f.run()
	require.NoError(t, err)

	result, dir, err := f.restore(time.Time{})
	require.NoError(t, err)
	require.Equal(t, wal.Ref.Hash, result.WAL.Hash)

	entries := tar.NewReader(bytes.NewReader(content))
	for {
		hdr, err := entries.Next()
		if err != nil {
			break
		}

		want := new(bytes.Buffer)
		_, _ = want.ReadFrom(entries)

		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(hdr.Name)))
		require.NoError(t, err)
		require.Equal(t, want.Bytes(), got, hdr.Name)
	}

	require.FileExists(t, filepath.Join(dir, "recovery.signal"))

	conf, err := os.ReadFile(filepath.Join(dir, "postgresql.auto.conf"))
	require.NoError(t, err)
	require.Contains(t, string(conf), "restore_command = 'goback postgres wal-get --commit "+hex.EncodeToString(wal.Ref.Hash)+" %f %p'")
	require.NotContains(t, string(conf), "recovery_target_time")
}

func TestARestoreToAMomentSetsItAsTheTarget(t *testing.T) {
	f := newWALFixture(t)

	_, err := f.runBase(baseTar(t, 42), nil)
	require.NoError(t, err)

	f.archive(7, 42)
	_, err = f.run()
	require.NoError(t, err)

	at := time.Now().Add(time.Hour)
	_, dir, err := f.restore(at)
	require.NoError(t, err)

	conf, err := os.ReadFile(filepath.Join(dir, "postgresql.auto.conf"))
	require.NoError(t, err)
	require.Contains(t, string(conf), "recovery_target_time = '"+at.UTC().Format("2006-01-02 15:04:05.999999+00")+"'")

	_, _, err = f.restore(time.Now().Add(-time.Hour))
	require.ErrorIs(t, err, ErrNoBase, "no base ended before then")
}

func TestARestoreRefusesABaseTheWALSetCannotContinue(t *testing.T) {
	f := newWALFixture(t)

	_, err := f.runBase(baseTar(t, 42), nil)
	require.NoError(t, err)

	f.archive(8, 42)
	_, err = f.run()
	require.NoError(t, err)

	_, _, err = f.restore(time.Time{})
	require.ErrorIs(t, err, ErrNoBase)
	require.ErrorContains(t, err, "before the WAL set's first file")
}

func TestARestoreRefusesABaseOfAnotherCluster(t *testing.T) {
	f := newWALFixture(t)

	_, err := f.runBase(baseTar(t, 42), nil)
	require.NoError(t, err)

	f.archive(7, 43)
	_, err = f.run()
	require.NoError(t, err)

	_, _, err = f.restore(time.Time{})
	require.ErrorIs(t, err, ErrNoBase)
	require.ErrorContains(t, err, "of cluster 42")
}

func TestARestoreWithoutWALRecoversOnlyItsBase(t *testing.T) {
	f := newWALFixture(t)

	_, err := f.runBase(baseTar(t, 42), nil)
	require.NoError(t, err)

	result, dir, err := f.restore(time.Time{})
	require.NoError(t, err)
	require.Nil(t, result.WAL)
	require.NoFileExists(t, filepath.Join(dir, "recovery.signal"))
}

func TestARestoreLeavesADirectoryInUseAlone(t *testing.T) {
	f := newWALFixture(t)

	_, err := f.runBase(baseTar(t, 42), nil)
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "PG_VERSION"), []byte("17"), 0o600))

	r := &Restore{Objects: f.index, Latest: func(ctx context.Context, set string) (*proto.Ref, error) { return f.index.LatestCommit(ctx, set) }, BaseSet: "db-base", WALSet: "db-wal"}
	_, err = r.Run(f.ctx, dir)
	require.ErrorContains(t, err, "not empty")
}

func TestFetchWALWritesWhatTheCommitHolds(t *testing.T) {
	f := newWALFixture(t)

	name := f.archive(7, 42)
	want, err := os.ReadFile(filepath.Join(f.spool.Dir, name))
	require.NoError(t, err)

	wal, err := f.run()
	require.NoError(t, err)

	dst := filepath.Join(t.TempDir(), "RECOVERYXLOG")
	require.NoError(t, FetchWAL(f.ctx, f.index, nil, wal.Ref, name, dst))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, want, got)

	err = FetchWAL(f.ctx, f.index, nil, wal.Ref, "00000002.history", dst)
	require.ErrorIs(t, err, backup.ErrNotFound)
}
