package packtest

import (
	"io/fs"
	"io/ioutil"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/twcclan/goback/storage/pack"

	"github.com/google/go-cmp/cmp"
)

func TestArchiveStorage(t *testing.T, store pack.ArchiveStorage) {
	type test struct {
		name string
		fn   func(t *testing.T, store pack.ArchiveStorage, files []file)
	}

	tests := []test{
		{"create files", testCreateFiles},
		{"read files", testReadFiles},
		{"missing file", testMissingFiles},
		{"list files", testListFiles},
	}

	// get test data
	files := getStorageTestFiles(t)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.fn(t, store, files)
		})
	}
}

func testCreateFiles(t *testing.T, store pack.ArchiveStorage, files []file) {
	// try to store all the files
	for _, f := range files {
		file, err := store.Create(f.key)
		if err != nil {
			t.Fatal(err)
		}

		n, err := file.Write(f.data)
		if err != nil {
			t.Fatal(err)
		}

		if n != len(f.data) {
			t.Fatal("didn't write full file content")
		}

		err = file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func testMissingFiles(t *testing.T, store pack.ArchiveStorage, files []file) {
	file, err := store.Open("this file probably does not exist")
	if err != pack.ErrFileNotFound {
		t.Fatal("expected open to return error")
	}

	if file != nil {
		t.Fatal("expected file to be nil")
	}
}

func testReadFiles(t *testing.T, store pack.ArchiveStorage, files []file) {
	for _, f := range files {
		file, err := store.Open(f.key)
		if err != nil {
			t.Fatal(err)
		}

		info, err := file.Stat()
		if err != nil {
			t.Fatal(err)
		}

		if diff := cmp.Diff(info.Name(), path.Base(f.key)); diff != "" {
			t.Error("file name doesn't match")
			t.Fatal(diff)
		}

		if diff := cmp.Diff(info.Size(), int64(len(f.data))); diff != "" {
			t.Error("file size doesn't match")
			t.Fatal(diff)
		}

		data, err := ioutil.ReadAll(file)
		if err != nil {
			t.Fatal(err)
		}

		if diff := cmp.Diff(data, f.data); diff != "" {
			t.Error("data doesn't match")
			t.Fatal(diff)
		}
	}
}

func testListFiles(t *testing.T, store pack.ArchiveStorage, files []file) {
	names, err := store.List()
	if err != nil {
		t.Fatal(err)
	}

	var expected []string
	for _, f := range files {
		if path.Ext(f.key) == ".goback" {

			name := strings.TrimSuffix(path.Base(f.key), path.Ext(f.key))
			expected = append(expected, name)
		}
	}

	if diff := cmp.Diff(expected, names); diff != "" {
		t.Error("unexpected list of files")
		t.Fatal(diff)
	}
}

type file struct {
	key  string
	data []byte
}

func getStorageTestFiles(t *testing.T) []file {
	t.Helper()

	// need to get path of current file to locate the testdata folder
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("couldn't get caller")
	}
	t.Log(filepath.FromSlash(filename))

	root := filepath.Join(filepath.Dir(filename), "testdata")

	var files []file

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// ignore folders
		if d.IsDir() {
			return nil
		}

		testKey := filepath.ToSlash(strings.TrimPrefix(p, root))[1:]

		data, err := ioutil.ReadFile(p)
		if err != nil {
			return err
		}

		files = append(files, file{
			key:  testKey,
			data: data,
		})

		return nil
	})

	if err != nil {
		t.Fatal(err)
	}

	if len(files) == 0 {
		t.Fatal("no test files loaded")
	}

	return files
}
