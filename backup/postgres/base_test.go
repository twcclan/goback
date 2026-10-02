package postgres

import (
	"archive/tar"
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"testing/iotest"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

// baseTar builds a tar as pg_basebackup -Ft streams it, of the cluster
// systemID.
func baseTar(t *testing.T, systemID uint64, entries ...string) []byte {
	t.Helper()

	control := make([]byte, 8192)
	binary.LittleEndian.PutUint64(control, systemID)

	relation := make([]byte, 1<<20)
	_, _ = rand.Read(relation)

	files := map[string][]byte{
		"backup_label":                    []byte(backupLabel),
		"global/pg_control":               control,
		"base/5/16384":                    relation,
		"pg_wal/000000010000000000000007": header(1, 7*segSize, systemID),
		"backup_manifest":                 []byte(backupManifest),
	}

	if len(entries) == 0 {
		entries = []string{"backup_label", "global/pg_control", "base/5/16384", "pg_wal/000000010000000000000007", "backup_manifest"}
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, name := range entries {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(files[name]))}))
		_, err := tw.Write(files[name])
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())

	return buf.Bytes()
}

func (f *walFixture) runBase(content []byte, wait func() error) (*backup.WalkResult, error) {
	b := &BaseBackup{Walker: &backup.Walker{Index: f.index, Objects: f.index, Set: "db-base", AgentID: "a", Workers: 1}}
	return b.Run(f.ctx, bytes.NewReader(content), wait)
}

func TestABaseBackupCommitsItsTarAndWhatItRecords(t *testing.T) {
	f := newWALFixture(t)
	content := baseTar(t, 42)

	result, err := f.runBase(content, nil)
	require.NoError(t, err)

	require.Equal(t, map[string]string{
		MetaSystemID:     "42",
		MetaTimeline:     "1",
		MetaStartLSN:     "0/7000028",
		MetaStopLSN:      "0/7000120",
		MetaStartWALFile: "000000010000000000000007",
	}, result.Commit.Metadata)

	tree, err := backup.OpenTree(f.ctx, f.index, result.Commit.Tree, nil, nil)
	require.NoError(t, err)
	require.Len(t, tree.Nodes, 1)
	require.Equal(t, BaseTar, string(tree.Nodes[0].Stat.Name))
	require.Equal(t, proto.NodeType_NODE_FILE, tree.Nodes[0].Stat.Type)
	require.EqualValues(t, len(content), tree.Nodes[0].Stat.Size)
}

func TestABaseBackupWhoseWriterFailedCommitsNothing(t *testing.T) {
	f := newWALFixture(t)
	failed := errors.New("pg_basebackup: exit status 1")

	_, err := f.runBase(baseTar(t, 42), func() error { return failed })
	require.ErrorIs(t, err, failed)

	_, err = f.index.LatestCommit(f.ctx, "db-base")
	require.ErrorIs(t, err, backup.ErrNotFound)
}

func TestABaseBackupWithoutItsManifestCommitsNothing(t *testing.T) {
	f := newWALFixture(t)

	_, err := f.runBase(baseTar(t, 42, "backup_label", "global/pg_control", "base/5/16384"), nil)
	require.ErrorContains(t, err, "backup_manifest")

	_, err = f.index.LatestCommit(f.ctx, "db-base")
	require.ErrorIs(t, err, backup.ErrNotFound)
}

func TestABaseBackupOfAnotherClusterIsRefused(t *testing.T) {
	f := newWALFixture(t)

	_, err := f.runBase(baseTar(t, 42), nil)
	require.NoError(t, err)

	_, err = f.runBase(baseTar(t, 43), nil)
	require.ErrorIs(t, err, ErrForeignCluster)
}

func TestABaseBackupWhoseStreamBreaksCommitsNothing(t *testing.T) {
	f := newWALFixture(t)
	broken := errors.New("broken pipe")

	b := &BaseBackup{Walker: &backup.Walker{Index: f.index, Objects: f.index, Set: "db-base", AgentID: "a", Workers: 1}}
	_, err := b.Run(f.ctx, io.MultiReader(bytes.NewReader(baseTar(t, 42)[:4096]), iotest.ErrReader(broken)), nil)
	require.ErrorIs(t, err, broken)

	_, err = f.index.LatestCommit(f.ctx, "db-base")
	require.ErrorIs(t, err, backup.ErrNotFound)
}

func TestABaseBackupThatIsNoTarIsReadToItsEndAndRefused(t *testing.T) {
	f := newWALFixture(t)

	garbage := make([]byte, 4<<20)
	_, _ = rand.Read(garbage)

	_, err := f.runBase(garbage, nil)
	require.ErrorContains(t, err, "reading the base backup")
}
