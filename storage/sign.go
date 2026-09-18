package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/storage/pack"

	gcs "cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"gocloud.dev/blob"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ pack.RangeSigner = (*BucketStore)(nil)

// rangeHeader is the header a signed range is bound to, so a URL cannot
// be turned on the rest of the archive.
const rangeHeader = "Range"

// bindRange puts the range among the headers the signature covers, for
// each driver that can, and reports whether one did.
func bindRange(as func(any) bool, value string) bool {
	var gcsOptions *gcs.SignedURLOptions
	if as(&gcsOptions) {
		gcsOptions.Headers = append(gcsOptions.Headers, rangeHeader+":"+value)

		return true
	}

	var s3Get *s3.GetObjectInput
	if as(&s3Get) {
		s3Get.Range = aws.String(value)

		return true
	}

	return false
}

// SignRange implements pack.RangeSigner. The URL is bound to the range,
// so the recipient can read those bytes and nothing else of the archive.
// A bucket whose driver cannot bind a header answers ErrNoSignedURL.
func (c *BucketStore) SignRange(ctx context.Context, name string, offset, length int64, ttl time.Duration) (*proto.Location, error) {
	if length <= 0 {
		return nil, fmt.Errorf("%w: empty range of %s", pack.ErrNoSignedURL, name)
	}

	value := fmt.Sprintf("bytes=%d-%d", offset, offset+length-1)

	bound := false
	url, err := c.bucket.SignedURL(ctx, c.key(name), &blob.SignedURLOptions{
		Expiry: ttl,
		Method: "GET",
		BeforeSign: func(as func(any) bool) error {
			bound = bindRange(as, value)

			return nil
		},
	})

	if err != nil {
		return nil, fmt.Errorf("%w: %v", pack.ErrNoSignedURL, err)
	}

	if !bound {
		return nil, fmt.Errorf("%w: this bucket cannot bind a range", pack.ErrNoSignedURL)
	}

	return &proto.Location{
		Url:     url,
		Header:  map[string]string{rangeHeader: value},
		Length:  length,
		Expires: timestamppb.New(time.Now().Add(ttl)),
	}, nil
}
