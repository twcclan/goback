package storage_test

import (
	"math/rand"
	"testing"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/storage"

	. "gopkg.in/check.v1"
)

// black box testing of storage drivers

func Test(t *testing.T) { TestingT(t) }

type StorageSuit struct{}

var _ = Suite(new(StorageSuit))

// number of objects to generate for the tests
const n = 1000

// average size of objects
const ObjectSize = 1024 * 8

func makeTestData(c *C, num int) []*proto.Object {
	c.Logf("Generating %d test objects", num)

	objects := make([]*proto.Object, num)

	for i := 0; i < num; i++ {
		size := rand.Int63n(ObjectSize * 2)
		randomBytes := make([]byte, size)
		_, err := rand.Read(randomBytes)
		if err != nil {
			c.Fatalf("Failed reading random data: %v", err)
		}

		objects[i] = proto.NewObject(&proto.Blob{
			Data: randomBytes,
		})
	}

	return objects
}

type opener interface {
	Open() error
}

type closer interface {
	Close() error
}

func (s *StorageSuit) testDriver(c *C, store backup.ObjectStore) {
	objects := makeTestData(c, n)

	if op, ok := store.(opener); ok {
		c.Log("Opening store")
		err := op.Open()
		if err != nil {
			c.Fatal(err)
		}
	}

	c.Logf("Storing %d objects", n)
	for _, object := range objects {
		err := store.Put(object)
		if err != nil {
			c.Fatal(err)
		}
	}

	c.Logf("Reading back %d objects", n)
	for _, i := range rand.Perm(n) {
		original := objects[i]

		object, err := store.Get(original.Ref())
		if err != nil {
			c.Fatal(err)
		}

		if object == nil {
			c.Fatal("Couldn't read back object")
		}

		c.Assert(object, DeepEquals, original)
	}

	if cl, ok := store.(closer); ok {
		c.Log("Closing store")
		err := cl.Close()
		if err != nil {
			c.Fatal(err)
		}
	}
}

func (s *StorageSuit) TestSimple(c *C) {
	s.testDriver(c, storage.NewSimpleObjectStore(c.MkDir()))
}

func (s *StorageSuit) TestPack(c *C) {
	s.testDriver(c, storage.NewPackStorage(storage.NewLocalArchiveStorage(c.MkDir())))
}

func (s *StorageSuit) TestMemory(c *C) {
	s.testDriver(c, storage.NewMemoryStore())
}
