package pack

import (
	"bytes"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// number of objects to generate for the tests
const numObjects = 1000

// average size of objects
const ObjectSize = 1024 * 8

type testingInterface interface {
	Logf(string, ...any)
	Fatalf(string, ...any)
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

func TestPack(t *testing.T) {
	base := t.TempDir()

	storage := newLocal(base)
	index := NewInMemoryIndex()
	txInfo := &proto.Transaction{
		TransactionId: "test",
		Status:        proto.Transaction_OPEN,
		CreatedAt:     timestamppb.Now(),
		UpdatedAt:     timestamppb.Now(),
	}

	options := []Option{
		WithArchiveStorage(storage),
		WithArchiveIndex(index),
		WithMaxSize(1024 * 1024 * 5),
	}

	store, err := New(options...)
	require.Nil(t, err)

	objects := makeTestData(t, numObjects)
	t.Logf("Storing %d objects", numObjects)

	objectsTx := makeTestData(t, numObjects)

	readObjects := func(t *testing.T, objects []*proto.Object) {
		t.Helper()

		t.Logf("Reading back %d objects", numObjects)
		for _, i := range rand.Perm(len(objects)) {
			original := objects[i]

			object, err := store.Get(context.Background(), original.Ref())
			if err != nil {
				if errors.Is(err, backup.ErrNotFound) {
					t.Fatalf("Couldn't find expected object %x", original.Ref().Sha1)
				} else {
					t.Fatal(err)
				}
			}

			if !bytes.Equal(object.Bytes(), original.Bytes()) {
				t.Logf("Original %x", original.Bytes())
				t.Logf("Stored %x", object.Bytes())
				t.Fatalf("Object read back incorrectly")
			}
		}
	}

	for _, object := range objects {
		err := store.Put(context.Background(), object)
		if err != nil {
			t.Fatal(err)
		}
	}

	require.Nil(t, store.Flush())

	readObjects(t, objects)

	tx, err := store.Transaction(context.Background(), txInfo)
	require.Nil(t, err)

	for _, object := range objectsTx {
		err := tx.Put(context.Background(), object)
		if err != nil {
			t.Fatal(err)
		}
	}

	require.Nil(t, tx.Prepare(context.Background()))
	require.Nil(t, tx.Commit(context.Background()))
	readObjects(t, objectsTx)

	t.Log("Closing pack store")
	err = store.Close()
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Reopening pack store")
	store, err = New(options...)
	require.Nil(t, err)

	readObjects(t, objects)
	readObjects(t, objectsTx)

	t.Log("Closing pack store")
	err = store.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func printIndex(t *testing.T, idx IndexFile) {
	for _, rec := range idx {
		t.Logf("hash: %x offset: %d", rec.Sum[:], rec.Offset)
	}
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

		err := store.Put(context.Background(), object)
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

func BenchmarkArchiveStorage(b *testing.B) {
	storage, err := New(WithArchiveStorage(newLocal(b.TempDir())))
	if err != nil {
		b.Fatal(err)
	}
	benchmarkStorage(b, storage)
}

func testIndex(t any) {
	// First ask Go to give us some information about the MyData type
	typ := reflect.TypeOf(t)
	fmt.Printf("Struct is %d bytes long\n", typ.Size())
	// We can run through the fields in the structure in order
	n := typ.NumField()
	for i := 0; i < n; i++ {
		field := typ.Field(i)
		fmt.Printf("%s at offset %v, size=%d, align=%d\n",
			field.Name, field.Offset, field.Type.Size(),
			field.Type.Align())
	}
}
