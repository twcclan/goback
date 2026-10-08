package storage

import (
	"testing"

	"github.com/gobackio/goback/storage/pack/packtest"

	"gocloud.dev/blob/fileblob"
)

func TestBucketStore(t *testing.T) {
	dir := t.TempDir()

	bucket, err := fileblob.OpenBucket(dir, nil) //memblob.OpenBucket(nil)
	if err != nil {
		t.Fatal(err)
	}

	packtest.TestArchiveStorage(t, NewBucketStore(bucket))

	t.Log(dir)
}
