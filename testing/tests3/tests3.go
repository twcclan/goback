// Package tests3 starts a throwaway S3-compatible object store for tests
// that need one.
package tests3

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	_ "gocloud.dev/blob/s3blob"
)

const (
	accessKey = "gobacktest"
	secretKey = "gobacktestsecret"
)

// Start runs a MinIO container holding one empty bucket and returns the
// blob URL that opens it; the test is skipped in short mode, and without
// Docker unless CI is set. It puts the credentials in the environment for the AWS SDK to
// find, so a test using it cannot run beside one wanting other ones.
func Start(t testing.TB, bucket string) string {
	t.Helper()

	if testing.Short() {
		t.Skip("short mode")
	}

	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "quay.io/minio/minio:RELEASE.2025-09-07T16-13-09Z",
			ExposedPorts: []string{"9000/tcp"},
			Env: map[string]string{
				"MINIO_ROOT_USER":     accessKey,
				"MINIO_ROOT_PASSWORD": secretKey,
			},
			Cmd:        []string{"server", "/data"},
			WaitingFor: wait.ForHTTP("/minio/health/ready").WithPort("9000/tcp"),
		},
		Started: true,
	})
	if err != nil {
		noDocker(t, err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "9000")
	require.NoError(t, err)

	t.Setenv("AWS_ACCESS_KEY_ID", accessKey)
	t.Setenv("AWS_SECRET_ACCESS_KEY", secretKey)

	endpoint := fmt.Sprintf("http://%s:%s", host, port.Port())

	_, err = client(endpoint).CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err, "creating the bucket")

	return fmt.Sprintf("s3://%s?endpoint=%s&region=us-east-1&use_path_style=true&disable_https=true",
		bucket, endpoint)
}

func client(endpoint string) *s3.Client {
	return s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
}

// noDocker skips the test, or fails it in CI, where Docker must be there.
func noDocker(t testing.TB, err error) {
	t.Helper()

	if os.Getenv("CI") != "" {
		t.Fatalf("no Docker: %v", err)
	}

	t.Skipf("no Docker: %v", err)
}
