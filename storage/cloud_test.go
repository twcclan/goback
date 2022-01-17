package storage

import (
	"testing"

	"github.com/twcclan/goback/storage/pack/packtest"

	"gocloud.dev/blob/fileblob"
)

func TestCloudStore(t *testing.T) {
	dir := t.TempDir()

	bucket, err := fileblob.OpenBucket(dir, nil) //memblob.OpenBucket(nil)
	if err != nil {
		t.Fatal(err)
	}

	packtest.TestArchiveStorage(t, NewCloudStore(bucket))

	t.Log(dir)
}
