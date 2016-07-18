package storage

import (
	"bytes"
	"io/ioutil"
	"math/rand"
	"testing"

	"github.com/twcclan/goback/proto"
)

func makeTestData(t *testing.T, num int) []*proto.Object {
	t.Logf("Generating %d test objects", num)

	objects := make([]*proto.Object, num)

	for i := 0; i < num; i++ {
		size := rand.Int63n(16 * 1024)
		randomBytes := make([]byte, size)
		_, err := rand.Read(randomBytes)
		if err != nil {
			t.Fatalf("Failed reading random data: %v", err)
		}

		objects[i] = proto.NewObject(&proto.Blob{
			Data: randomBytes,
		})
	}

	return objects
}

func getTempDir(t *testing.T) string {
	dir, err := ioutil.TempDir("", "goback")

	if err != nil {
		panic(err)
	}

	t.Logf("Creating temporary dir %s", dir)

	return dir
}

func TestPack(t *testing.T) {
	n := 10000
	base := getTempDir(t)
	store := NewPackStorage(base)

	objects := makeTestData(t, n)
	t.Logf("Storing %d objects", n)

	for _, object := range objects {
		err := store.Put(object)
		if err != nil {
			t.Fatal(err)
		}
	}

	t.Logf("Reading back %d objects", n)
	for _, i := range rand.Perm(n) {
		original := objects[i]

		if !store.Has(original.Ref()) {
			t.Fatal("Archive doesn't have object")
		}

		object, err := store.Get(original.Ref())
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(object.Bytes(), original.Bytes()) {
			t.Fatal("Object read back incorrectly")
		}
	}

	err := store.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestArchive(t *testing.T) {
	// number of objects to generate
	n := 10000
	base := getTempDir(t)

	t.Logf("Creating archive in %s", base)
	a, err := newArchive(base)
	if err != nil {
		t.Fatal(err)
	}

	objects := makeTestData(t, n)

	t.Logf("Storing %d objects", n)
	for _, object := range objects {
		err = a.Put(object)
		if err != nil {
			t.Fatal(err)
		}
	}

	readBack := func() {
		for _, i := range rand.Perm(n) {
			original := objects[i]

			if !a.Has(original.Ref()) {
				t.Fatal("Archive doesn't have object")
			}

			object, err := a.Get(original.Ref())
			if err != nil {
				t.Fatal(err)
			}

			if !bytes.Equal(object.Bytes(), original.Bytes()) {
				t.Fatal("Object read back incorrectly")
			}
		}
	}

	t.Log("Reading back stored objects from open archive")
	readBack()

	t.Log("Closing archive")
	err = a.Close()
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Reading back from finalized archive")
	readBack()

	t.Log("Closing reader")
	err = a.CloseReader()
	if err != nil {
		t.Fatal(err)
	}

	path := a.archive.Name() + ArchiveSuffix

	t.Logf("Re-opening archive from %s", path)
	a, err = openArchive(path)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Reading back stored objects from readonly archive")
	readBack()
}
