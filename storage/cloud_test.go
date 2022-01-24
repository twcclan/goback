package storage

import (
	"testing"

	"github.com/twcclan/goback/storage/pack/packtest"

	"gocloud.dev/blob/memblob"
)

func TestCloudStore(t *testing.T) {
	bucket := memblob.OpenBucket(nil)

	packtest.TestArchiveStorage(t, NewCloudStore(bucket))
}
