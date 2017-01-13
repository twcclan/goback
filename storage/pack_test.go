package storage

import (
	"bytes"
	"crypto/sha1"
	"io/ioutil"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
)

// number of objects to generate for the tests
const n = 1000

// average size of objects
const ObjectSize = 1024 * 8

type testingInterface interface {
	Logf(string, ...interface{})
}

func makeRef() *proto.Ref {
	hash := make([]byte, sha1.Size)
	_, err := rand.Read(hash)
	if err != nil {
		panic(err)
	}

	return &proto.Ref{
		Sha1: hash,
	}
}

func makeTestData(t *testing.T, num int) []*proto.Object {
	t.Logf("Generating %d test objects", num)

	objects := make([]*proto.Object, num)

	for i := 0; i < num; i++ {
		size := rand.Int63n(ObjectSize * 2)
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

func getTempDir(t testingInterface) string {
	dir, err := ioutil.TempDir("", "goback")

	if err != nil {
		panic(err)
	}

	//t.Logf("Creating temporary dir %s", dir)

	return dir
}

func TestPack(t *testing.T) {
	base := getTempDir(t)
	storage := NewLocalArchiveStorage(base)
	store := NewPackStorage(storage)

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
			t.Logf("Original %x", original.Bytes())
			t.Logf("Stored %x", object.Bytes())
			t.Fatalf("Object read back incorrectly")
		}
	}

	err := store.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func printIndex(t *testing.T, idx index) {
	for _, rec := range idx {
		t.Logf("hash: %x offset: %d", rec.Sum[:], rec.Offset)
	}
}

func TestIndex(t *testing.T) {
	buf := new(bytes.Buffer)
	idx := make(index, 1000)

	for i := range idx {
		idx[i].Offset = rand.Uint32()
		for j := range idx[i].Sum {
			idx[i].Sum[j] = byte(rand.Intn(256))
		}
	}
	_, err := idx.WriteTo(buf)
	assert.Nil(t, err)

	var newIdx index

	_, err = (&newIdx).ReadFrom(bytes.NewReader(buf.Bytes()))
	assert.Nil(t, err)

	assert.Equal(t, idx, newIdx)
}

func TestArchive(t *testing.T) {
	base := getTempDir(t)
	storage := NewLocalArchiveStorage(base)

	t.Logf("Creating archive in %s", base)
	a, err := newArchive(storage)
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
				t.Logf("Original %x", original.Bytes())
				t.Logf("Stored %x", object.Bytes())
				t.Fatalf("Object read back incorrectly")
			}
		}
	}

	t.Log("Reading back stored objects from open archive")
	readBack()

	t.Log("Closing archive")
	err = a.CloseWriter()
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

	path := a.name

	t.Logf("Re-opening archive from %s", path)
	a, err = openArchive(storage, a.name)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Reading back stored objects from readonly archive")
	readBack()
}

/*
func BenchmarkReadIndex(b *testing.B) {
	locs := make([]*proto.Location, b.N)
	for i := 0; i < b.N; i++ {
		locs[i] = &proto.Location{
			Ref:    makeRef(),
			Offset: uint64(rand.Int63()),
			Size:   uint64(rand.Int63()),
			Type:   proto.ObjectType_INVALID,
		}
	}

	sort.Sort(byRef(locs))

	idx := &proto.Index{
		Locations: locs,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for _, i := range rand.Perm(b.N) {
		loc := idx.Lookup(locs[i].Ref)
		if loc == nil {
			panic("Could not find location in index")
		}
	}
}
*/

var benchRnd = rand.New(rand.NewSource(0))

type Opener interface {
	Open() error
}

type Closer interface {
	Close() error
}

func benchmarkStorage(b *testing.B, store backup.ObjectStore) {
	benchRnd.Seed(0)
	b.ReportAllocs()

	if op, ok := store.(Opener); ok {
		err := op.Open()
		if err != nil {
			b.Fatal(err)
		}
	}

	for i := 0; i < b.N; i++ {
		blobBytes := make([]byte, benchRnd.Intn(16*1024))
		_, _ = benchRnd.Read(blobBytes)
		object := proto.NewObject(&proto.Blob{
			Data: blobBytes,
		})

		err := store.Put(object)
		if err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(len(blobBytes)))
	}

	if cl, ok := store.(Closer); ok {
		err := cl.Close()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMemoryStorage(b *testing.B) {
	benchmarkStorage(b, NewMemoryStore())
}

func BenchmarkSimpleStorage(b *testing.B) {
	benchmarkStorage(b, NewSimpleObjectStore(getTempDir(b)))
}

func BenchmarkArchiveStorage(b *testing.B) {
	benchmarkStorage(b, NewPackStorage(NewLocalArchiveStorage(getTempDir(b))))
}
