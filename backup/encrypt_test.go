package backup

import (
	"bytes"
	"context"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

func newKey(t *testing.T) *storekey.Key {
	t.Helper()

	key, err := storekey.Generate("s1")
	require.NoError(t, err)

	return key
}

// readAll restores every file under the tree through the reader.
func readAll(t *testing.T, reader *BackupReader, tree *proto.Ref) map[string][]byte {
	t.Helper()

	files := map[string][]byte{}
	err := reader.WalkTree(context.Background(), tree, nil, func(path string, info os.FileInfo, ref *proto.Ref) error {
		if info.IsDir() {
			return nil
		}

		r, err := reader.ReadFile(context.Background(), ref)
		if err != nil {
			return err
		}

		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}

		files[filepath.ToSlash(path)] = data
		return nil
	})
	require.NoError(t, err)

	return files
}

// fileObject finds a file's object under an encrypted tree by its plain path.
func fileObject(t *testing.T, store ObjectStore, key *storekey.Key, tree *proto.Ref, path string) *proto.File {
	t.Helper()

	reader := NewBackupReader(store).WithKey(key)
	dir, name := filepath.Split(path)

	var parts []string
	if dir != "" {
		parts = []string{filepath.Clean(dir)}
	}

	ref, parent, err := reader.GetTree(context.Background(), tree, parts)
	require.NoError(t, err)

	nodes, err := OpenTree(context.Background(), store, ref, key, parent)
	require.NoError(t, err)

	for _, node := range nodes.Nodes {
		if string(node.Stat.Name) == name {
			obj, err := store.Get(context.Background(), node.Ref)
			require.NoError(t, err)
			return obj.GetFile()
		}
	}

	t.Fatalf("%s not found", path)
	return nil
}

func TestWalkerWithKeySealsNamesAndContents(t *testing.T) {
	f := newWalkerFixture(t)
	key := newKey(t)
	f.walker.Key = key

	contents := map[string][]byte{
		"a.txt":     []byte("hello"),
		"sub/b.bin": f.random(150 << 10),
		"sub/c.dat": bytes.Repeat([]byte("game save data "), 20000),
	}
	for path, data := range contents {
		f.write(path, data)
	}

	first := f.run()
	require.EqualValues(t, 1, first.Commit.PolicyVersion)

	stored, err := LoadTree(context.Background(), f.store, first.Commit.Tree)
	require.NoError(t, err)
	for _, node := range stored.Nodes {
		require.NotContains(t, []string{"a.txt", "sub"}, string(node.Stat.Name), "stored names are tokens")
	}

	f.store.mtx.RLock()
	for _, obj := range f.store.objects {
		switch obj.Type() {
		case proto.ObjectType_BLOB:
			require.NotNil(t, obj.GetSealed(), "every blob is sealed")
		case proto.ObjectType_FILE:
			require.NotEmpty(t, obj.GetFile().Keys, "every file carries its part keys")
		}
	}
	f.store.mtx.RUnlock()

	require.Equal(t, contents, readAll(t, NewBackupReader(f.store).WithKey(key), first.Commit.Tree))

	// without the key the tree still walks, but names are tokens and
	// contents do not open
	plain := NewBackupReader(f.store)
	err = plain.WalkTree(context.Background(), first.Commit.Tree, nil, func(path string, info os.FileInfo, ref *proto.Ref) error {
		require.NotContains(t, []string{"a.txt", "sub"}, filepath.Base(path))
		if info.IsDir() {
			return nil
		}

		r, err := plain.ReadFile(context.Background(), ref)
		require.NoError(t, err)
		_, err = io.ReadAll(r)
		require.ErrorIs(t, err, storekey.ErrNoKey)
		return nil
	})
	require.NoError(t, err)

	err = NewBackupReader(f.store).WithKey(newKey(t)).WalkTree(context.Background(), first.Commit.Tree, nil, func(string, os.FileInfo, *proto.Ref) error { return nil })
	require.ErrorIs(t, err, storekey.ErrWrongKey)

	// the modes follow the policy
	require.Equal(t, proto.Encryption_CONVERGENT, blobMode(t, f.store, fileObject(t, f.store, key, first.Commit.Tree, "sub/b.bin")))
	require.Equal(t, proto.Encryption_STORE_KEYED, blobMode(t, f.store, fileObject(t, f.store, key, first.Commit.Tree, "sub/c.dat")))
	require.NotEmpty(t, fileObject(t, f.store, key, first.Commit.Tree, "a.txt").Inline)

	// an unchanged run is detected through the opened base tree
	second := f.run()
	require.EqualValues(t, 3, second.Reused)
	require.EqualValues(t, 0, second.Read)
	require.True(t, second.Commit.Tree.Equal(first.Commit.Tree))

	f.write("sub/c.dat", []byte("changed"))
	contents["sub/c.dat"] = []byte("changed")

	third := f.run()
	require.EqualValues(t, 1, third.Read)
	require.Equal(t, contents, readAll(t, NewBackupReader(f.store).WithKey(key), third.Commit.Tree))

	ref, err := HashFile(filepath.Join(f.root, "sub", "b.bin"), key)
	require.NoError(t, err)
	require.True(t, ref.Equal(proto.NewObject(fileObject(t, f.store, key, third.Commit.Tree, "sub/b.bin")).Ref()))
}

func blobMode(t *testing.T, store ObjectStore, file *proto.File) proto.Encryption {
	t.Helper()

	require.NotEmpty(t, file.Parts)
	obj, err := store.Get(context.Background(), file.Parts[0].Ref)
	require.NoError(t, err)
	require.NotNil(t, obj.GetSealed())

	return obj.GetSealed().Encryption
}

func TestConvergentBlobsDedupAcrossStores(t *testing.T) {
	data := make([]byte, 400<<10)
	rand.New(rand.NewSource(7)).Read(data)

	var files []*proto.File
	for i := 0; i < 2; i++ {
		f := newWalkerFixture(t)
		key := newKey(t)
		f.walker.Key = key
		f.write("shared.dat", data)
		f.write("secret.dat", data[:1000])

		result := f.run()
		files = append(files, fileObject(t, f.store, key, result.Commit.Tree, "shared.dat"))

		secret := fileObject(t, f.store, key, result.Commit.Tree, "secret.dat")
		require.NotEqual(t, files[0].Parts[0].Ref.Hash, secret.Inline)
	}

	require.Equal(t, len(files[0].Parts), len(files[1].Parts))
	for i := range files[0].Parts {
		require.True(t, files[0].Parts[i].Ref.Equal(files[1].Parts[i].Ref), "convergent blobs share refs across stores")
	}

	require.NotEqual(t, files[0].Keys, files[1].Keys, "the key lists are sealed per store")
}

func TestIndexPath(t *testing.T) {
	key := newKey(t)

	require.Equal(t, "a/b.txt", IndexPath(nil, "/a/b.txt/"))
	require.Equal(t, "", IndexPath(key, ""))

	sub := NameToken(key, nil, []byte("sub"))
	want := proto.JoinPath(proto.JoinPath("", sub), NameToken(key, sub, []byte("b.bin")))
	require.Equal(t, want, IndexPath(key, "sub/b.bin"))
	require.Equal(t, []byte("sub"), func() []byte {
		name, err := key.OpenField(nil, storekey.FieldName, proto.NameFromComponent(proto.PathComponent(sub)))
		require.NoError(t, err)
		return name
	}())
}
