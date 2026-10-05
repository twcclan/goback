// Package testpg gives tests a throwaway Postgres database.
package testpg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	once      sync.Once
	server    string
	serverErr error
	databases atomic.Int64
)

// Start returns the superuser DSN of a new, empty database of its own,
// dropped when the test ends. Every test of a binary shares one Postgres
// container. The test is skipped in short mode, and without Docker unless
// CI is set.
func Start(t testing.TB) string {
	t.Helper()

	dsn, _ := start(t)

	return dsn
}

func start(t testing.TB) (string, string) {
	t.Helper()

	if testing.Short() {
		t.Skip("short mode")
	}

	once.Do(func() { server, _, serverErr = launch(context.Background()) })
	if serverErr != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("no Docker: %v", serverErr)
		}

		t.Skipf("no Docker: %v", serverErr)
	}

	name := fmt.Sprintf("test_%d", databases.Add(1))
	require.NoError(t, exec("CREATE DATABASE "+name))
	t.Cleanup(func() { _ = exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)") })

	return fmt.Sprintf(server, name), name
}

// WithApp is Start, and the DSN of a role that may only create schemas in
// that database, as the servers' own role may.
func WithApp(t testing.TB) (superuser, app string) {
	t.Helper()

	superuser, name := start(t)
	role := name + "_app"

	require.NoError(t, exec("CREATE ROLE "+role+" LOGIN PASSWORD 'app'"))
	require.NoError(t, exec("GRANT CREATE ON DATABASE "+name+" TO "+role))
	t.Cleanup(func() {
		_ = exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
		_ = exec("DROP ROLE IF EXISTS " + role)
	})

	return superuser, strings.Replace(superuser, "postgres:goback@", role+":app@", 1)
}

func exec(statement string) error {
	db, err := sql.Open("pgx", fmt.Sprintf(server, "postgres"))
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(statement)

	return err
}

// Container starts a Postgres server of its own, outside any test, and
// returns the DSN of its empty postgres database and a func that stops it.
func Container(ctx context.Context) (string, func(), error) {
	dsn, stop, err := launch(ctx)
	if err != nil {
		return "", nil, err
	}

	return fmt.Sprintf(dsn, "postgres"), stop, nil
}

// launch starts a container and returns its DSN with the database left as
// a %s verb, and a func that stops it.
func launch(ctx context.Context) (string, func(), error) {
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:17-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env:          map[string]string{"POSTGRES_PASSWORD": "goback"},
			Cmd:          []string{"postgres", "-c", "fsync=off", "-c", "max_connections=1000"},
			WaitingFor:   wait.ForListeningPort("5432/tcp"),
		},
		Started: true,
	})
	if err != nil {
		return "", nil, err
	}

	stop := func() { _ = container.Terminate(context.Background()) }

	dsn, err := ready(ctx, container)
	if err != nil {
		stop()

		return "", nil, err
	}

	return dsn, stop, nil
}

func ready(ctx context.Context, container testcontainers.Container) (string, error) {
	host, err := container.Host(ctx)
	if err != nil {
		return "", err
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		return "", err
	}

	dsn := "postgres://postgres:goback@" + host + ":" + port.Port() + "/%s?sslmode=disable"

	db, err := sql.Open("pgx", fmt.Sprintf(dsn, "postgres"))
	if err != nil {
		return "", err
	}
	defer db.Close()

	for deadline := time.Now().Add(time.Minute); db.PingContext(ctx) != nil; time.Sleep(200 * time.Millisecond) {
		if time.Now().After(deadline) {
			return "", errors.New("postgres did not come up")
		}
	}

	return dsn, nil
}
