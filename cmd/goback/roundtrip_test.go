package main

import (
	"bytes"
	"crypto/rand"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// buildCLI compiles the binary once per test run.
func buildCLI(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "goback.exe")
	build := exec.Command("go", "build", "-o", bin, ".")
	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))

	return bin
}

type tool struct {
	t    *testing.T
	bin  string
	home string
}

// run invokes the binary against the test's store and index; a failure
// reports the combined output.
func (c *tool) run(args ...string) string {
	c.t.Helper()

	all := append([]string{"--storage", "store", "--index", "index", "--set", "world", "--agent-id", "node-1"}, args...)
	cmd := exec.Command(c.bin, all...)
	cmd.Dir = c.home
	out, err := cmd.CombinedOutput()
	require.NoError(c.t, err, "%s\n%s", strings.Join(all, " "), out)

	return string(out)
}

// snapshot lists every file under root with its content, and every
// directory, as relative slash paths.
func snapshot(t *testing.T, root string) map[string][]byte {
	t.Helper()

	got := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		if path == root {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			got[rel+"/"] = nil
			return nil
		}

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		got[rel] = data

		return nil
	})
	require.NoError(t, err)

	return got
}

func names(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)

	return out
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, data, 0o644))
}

// TestCommitAndRestoreRoundTrip backs a directory up with the binary,
// damages it, and restores it in place with --delete and into an empty
// directory, expecting both to match the original byte for byte.
func TestCommitAndRestoreRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}

	home := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(home, "store"), 0o755))
	c := &tool{t: t, bin: buildCLI(t), home: home}

	src := filepath.Join(home, "world")
	big := make([]byte, 3<<20)
	_, err := rand.Read(big)
	require.NoError(t, err)

	write(t, filepath.Join(src, "server.properties"), []byte("motd=hello\n"))
	write(t, filepath.Join(src, "region", "r.0.0.mca"), big)
	write(t, filepath.Join(src, "region", "r.0.1.mca"), bytes.Repeat([]byte("chunk"), 100_000))
	write(t, filepath.Join(src, "plugins", "config", "a.yml"), []byte("a: 1\n"))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "logs"), 0o755))

	want := snapshot(t, src)

	c.run("commit", "new", src)

	// damage: one file changed, one deleted, one added, one stray directory
	write(t, filepath.Join(src, "server.properties"), []byte("motd=changed\n"))
	require.NoError(t, os.Remove(filepath.Join(src, "region", "r.0.1.mca")))
	write(t, filepath.Join(src, "plugins", "stray.jar"), []byte("stray"))
	write(t, filepath.Join(src, "crash-reports", "crash.txt"), []byte("boom"))

	c.run("commit", "restore", "--delete", src)
	got := snapshot(t, src)
	require.Equal(t, names(want), names(got), "an exact restore removes what the commit does not hold")
	require.Equal(t, want, got)

	// a second commit of the restored tree changes nothing
	c.run("commit", "new", src)

	fresh := filepath.Join(home, "fresh")
	c.run("commit", "restore", fresh)
	require.Equal(t, want, snapshot(t, fresh), "a restore into an empty directory reproduces the tree")

	// without --delete the restore is additive
	write(t, filepath.Join(fresh, "extra.txt"), []byte("kept"))
	c.run("commit", "restore", fresh)
	got = snapshot(t, fresh)
	require.Equal(t, []byte("kept"), got["extra.txt"])
	delete(got, "extra.txt")
	require.Equal(t, want, got)
}
