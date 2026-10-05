package storage

import (
	"testing"

	gcs "cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
)

func TestCloudStorageSignsTheExtraHeadersBesideTheRange(t *testing.T) {
	options := &gcs.SignedURLOptions{}
	as := func(target any) bool {
		if p, ok := target.(**gcs.SignedURLOptions); ok {
			*p = options
			return true
		}

		return false
	}

	require.True(t, bind(as, "bytes=0-5", map[string]string{"x-goog-custom-audit-goback": "t1"}))
	require.ElementsMatch(t, []string{"Range:bytes=0-5", "x-goog-custom-audit-goback:t1"}, options.Headers)
}

func TestS3CannotCarryExtraHeaders(t *testing.T) {
	as := func(target any) bool {
		if p, ok := target.(**s3.GetObjectInput); ok {
			*p = &s3.GetObjectInput{}
			return true
		}

		return false
	}

	require.True(t, bind(as, "bytes=0-5", nil))
	require.False(t, bind(as, "bytes=0-5", map[string]string{"x-mark": "t1"}), "an S3 URL would not bind the header")
}
