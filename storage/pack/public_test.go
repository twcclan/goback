package pack

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoutedStorageSplitsThePublicPrefix(t *testing.T) {
	rootDir, publicDir := t.TempDir(), t.TempDir()
	routed := &routedStorage{root: newLocal(rootDir), public: newLocal(publicDir)}

	for _, name := range []string{"a.goback", "public/b.goback", "s1/c.goback"} {
		file, err := routed.Create(name)
		require.NoError(t, err)
		require.NoError(t, file.Close())

		file, err = routed.Open(name)
		require.NoError(t, err)
		require.NoError(t, file.Close())
	}

	names, err := routed.List(".goback")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"a.goback", "s1/c.goback", "public/b.goback"}, names)

	matches, err := filepath.Glob(filepath.Join(publicDir, "*.goback"))
	require.NoError(t, err)
	require.Len(t, matches, 1, "the public archive sits at the public storage's root")

	matches, err = filepath.Glob(filepath.Join(rootDir, placementPublic, "*"))
	require.NoError(t, err)
	require.Empty(t, matches, "and not under the root")

	require.NoError(t, routed.Delete("public/b.goback"))
	names, err = routed.List(".goback")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"a.goback", "s1/c.goback"}, names)

	require.NoError(t, routed.DeleteAll())
	names, err = routed.List(".goback")
	require.NoError(t, err)
	require.Empty(t, names)
}

func TestPublicStorageHoldsThePublicPrefix(t *testing.T) {
	root, public := t.TempDir(), t.TempDir()
	shared := newLocal(public)
	store := newTestStore(t, root, WithMaxParallel(4), WithPublicStorage(shared), WithCompaction(CompactionConfig{MinimumCandidates: 0}))

	blob := convergentBlob(t, "the same chunk everywhere")
	private := makeTestData(t, 1)[0]

	ctx, _ := beginSession(t, store, "agent-1")
	require.NoError(t, store.Put(ctx, blob))
	require.NoError(t, store.Put(ctx, private))
	require.NoError(t, store.Put(ctx, commitObject()))
	require.NoError(t, store.Compact())
	require.Equal(t, []PlacementKind{PlacementPublic, PlacementRoot}, placements(t, store, blob, private))

	names, err := shared.List(ArchiveSuffix)
	require.NoError(t, err)
	require.Len(t, names, 1, "the public archive lives in the public storage")
	require.NotContains(t, names[0], "/")

	matches, err := filepath.Glob(filepath.Join(root, placementPublic, "*"))
	require.NoError(t, err)
	require.Empty(t, matches, "and not under the root")

	var refs [][]byte
	require.NoError(t, store.WalkPublicRefs(func(ref []byte) error {
		refs = append(refs, ref)
		return nil
	}))
	require.Equal(t, [][]byte{blob.Ref().Hash}, refs)

	got, err := store.Get(context.Background(), blob.Ref())
	require.NoError(t, err)
	require.Equal(t, blob.GetSealed().Data, got.GetSealed().Data)

	require.NoError(t, store.Close())

	store = newTestStore(t, root, WithPublicStorage(shared))
	require.Equal(t, []PlacementKind{PlacementPublic, PlacementRoot}, placements(t, store, blob, private), "reopening lists both storages")

	require.NoError(t, store.Close())
	require.NoError(t, (&routedStorage{root: newLocal(root), public: shared}).DeleteAll())
	names, err = shared.List(ArchiveSuffix)
	require.NoError(t, err)
	require.Len(t, names, 1, "deleting the store leaves the shared public storage alone")
}
