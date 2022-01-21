package pack

import (
	"bytes"
	"context"
	"math/rand"
	"sync"
	"testing"

	"github.com/twcclan/goback/proto"
)

func TestReaderWriter(t *testing.T) {
	base := t.TempDir()

	storage := newLocal(base)
	index := NewInMemoryIndex()
	objects := makeTestData(t, numObjects)

	t.Run("writer", func(t *testing.T) {
		writer, err := NewWriter(storage, index, 5, 1024*1024*1024)
		if err != nil {
			t.Fatal(err)
		}

		wg := sync.WaitGroup{}

		for _, object := range objects {
			wg.Add(1)
			go func(obj *proto.Object) {
				defer wg.Done()

				err := writer.Put(context.Background(), obj)
				if err != nil {
					t.Error(err)
					return
				}
			}(object)
		}

		wg.Wait()

		err = writer.Close()
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("reader", func(t *testing.T) {
		reader := NewReader(storage, index)

		for _, i := range rand.Perm(numObjects) {
			original := objects[i]

			object, err := reader.Get(context.Background(), original.Ref())
			if err != nil {
				t.Fatal(err)
			}

			if object == nil {
				t.Fatalf("Couldn't find expected object %x", original.Ref().Sha1)
			}

			if !bytes.Equal(object.Bytes(), original.Bytes()) {
				t.Logf("Original %x", original.Bytes())
				t.Logf("Stored %x", object.Bytes())
				t.Fatalf("Object read back incorrectly")
			}
		}
	})
}
