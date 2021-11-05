package crdb

import (
	"context"
	"fmt"
	"testing"

	"github.com/twcclan/goback/storage/pack/packtest"

	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type crdbContainer struct {
	testcontainers.Container
	URI string
}

func withCrdb(tb testing.TB, ctx context.Context) *Index {
	tb.Helper()
	tb.Logf("creating temporary crdb node")

	req := testcontainers.ContainerRequest{
		Image:        "cockroachdb/cockroach:latest-v21.1",
		ExposedPorts: []string{"26257/tcp", "8080/tcp"},
		WaitingFor:   wait.ForHTTP("/health").WithPort("8080"),
		Cmd:          []string{"start-single-node", "--insecure"},
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		tb.Fatalf("failed setting up crdb node: %v", err)
	}

	tb.Cleanup(func() {
		tb.Logf("removing temporary crdb node")
		err := container.Terminate(context.Background())
		if err != nil {
			tb.Logf("warning: failed terminating crdb node: %v", err)
		}
	})

	mappedPort, err := container.MappedPort(ctx, "26257")
	if err != nil {
		tb.Fatalf("failed getting mapped port: %v", err)
	}

	hostIP, err := container.Host(ctx)
	if err != nil {
		tb.Fatalf("failed getting host ip: %v", err)
	}

	uri := fmt.Sprintf("postgres://root@%s:%s/defaultdb?sslmode=disable", hostIP, mappedPort.Port())

	idx := New(uri)

	err = idx.Open()
	if err != nil {
		tb.Fatal(err)
	}

	tb.Cleanup(func() {
		err := idx.Close()
		if err != nil {
			tb.Logf("warning: failed closing sql connection: %v", err)
		}
	})

	return idx
}

func TestCRDB(t *testing.T) {
	packtest.TestArchiveIndex(t, withCrdb(t, context.Background()))
}

func BenchmarkLookup(b *testing.B) {
	idx := withCrdb(b, context.Background())

	b.Run("lookup", func(b *testing.B) {
		packtest.BenchmarkLookup(b, idx)
	})
}
