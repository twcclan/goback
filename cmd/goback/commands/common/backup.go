package common

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/twcclan/goback/auth"
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/retention"
	"github.com/twcclan/goback/backup/storekey"
	badgerIdx "github.com/twcclan/goback/index/badger"
	"github.com/twcclan/goback/index/sql"
	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/storage"
	"github.com/twcclan/goback/storage/badger"
	"github.com/twcclan/goback/storage/pack"

	"github.com/urfave/cli"
	"gocloud.dev/blob"
	"gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/gcsblob"
)

type Opener interface {
	Open() error
}

type Closer interface {
	Close() error
}

// StoreKey loads the key file named by the global --store-key flag, or
// returns nil when the store is written in the clear.
func StoreKey(c *cli.Context) *storekey.Key {
	path := c.GlobalString("store-key")
	if path == "" {
		return nil
	}

	key, err := storekey.Load(path)
	if err != nil {
		log.Fatalf("Could not load store key: %v", err)
	}

	return key
}

// Unwrap peels caching and wrapping stores off until the innermost store.
func Unwrap(store backup.ObjectStore) backup.ObjectStore {
	for {
		wrapper, ok := store.(interface{ Unwrap() backup.ObjectStore })
		if !ok {
			return store
		}

		store = wrapper.Unwrap()
	}
}

func createFolders(loc string) (string, error) {
	abs, err := filepath.Abs(loc)
	if err != nil {
		return "", err
	}

	err = os.MkdirAll(abs, os.FileMode(0700))
	if err != nil {
		if os.IsExist(err) {
			return abs, err
		}
	}

	return abs, nil
}

func makeLocation(u *url.URL) (string, error) {
	return createFolders(u.Host + u.Path)
}

func initSimple(u *url.URL, c *cli.Context) (backup.ObjectStore, error) {
	loc, err := makeLocation(u)

	return storage.NewSimpleObjectStore(loc), err
}

func initPack(u *url.URL, c *cli.Context) (backup.ObjectStore, error) {
	archiveLocation, err := makeLocation(u)
	if err != nil {
		return nil, err
	}

	indexLocation, err := createFolders(filepath.Join(archiveLocation, "index"))
	if err != nil {
		return nil, err
	}

	idx, err := badgerIdx.NewBadgerIndex(indexLocation)
	if err != nil {
		return nil, err
	}

	if c.GlobalBool("reset-index") {
		log.Printf("Resetting archive index at %s", indexLocation)

		err = idx.Reset()
		if err != nil {
			return nil, err
		}
	}

	file, err := fileblob.OpenBucket(archiveLocation, nil)
	if err != nil {
		return nil, err
	}

	options, err := atRest(c)
	if err != nil {
		return nil, err
	}

	return pack.NewPackStorage(append(options,
		pack.WithArchiveStorage(storage.NewBucketStore(file)),
		pack.WithArchiveIndex(idx),
		pack.WithMaxParallel(1),
		pack.WithMaxSize(1024*1024*1024),
		pack.WithSessionLease(10*time.Minute),
		pack.WithCompaction(pack.CompactionConfig{
			OnClose:           true,
			MinimumCandidates: 100,
		}),
	)...)
}

// atRest is the pack option for the global --at-rest-key flag, none
// without it.
func atRest(c *cli.Context) ([]pack.PackOption, error) {
	path := c.GlobalString("at-rest-key")
	if path == "" {
		return nil, nil
	}

	key, err := storekey.Load(path)
	if err != nil {
		return nil, fmt.Errorf("loading the at-rest key: %w", err)
	}

	return []pack.PackOption{pack.WithAtRestKey(key)}, nil
}

func initGCS(u *url.URL, c *cli.Context) (backup.ObjectStore, error) {
	bucket, err := blob.OpenBucket(context.Background(), "gs://"+u.Host)
	if err != nil {
		return nil, err
	}

	options, err := atRest(c)
	if err != nil {
		return nil, err
	}

	return storage.NewBucketObjectStore(bucket, u.Query().Get("index"), u.Query().Get("cache"), options...)
}

func initRemote(u *url.URL, c *cli.Context) (backup.ObjectStore, error) {
	port := u.Port()
	if port == "" {
		port = "6060"
	}

	addr := net.JoinHostPort(u.Host, port)

	tlsConfig, err := storage.ClientTLS(c.GlobalString("ca-cert"))
	if err != nil {
		return nil, err
	}

	return storage.NewRemoteClient(addr, auth.Credentials{Secret: c.GlobalString("secret"), AgentID: AgentID(c)}, tlsConfig)
}

func initBadger(u *url.URL, c *cli.Context) (backup.ObjectStore, error) {
	loc, err := makeLocation(u)
	if err != nil {
		return nil, err
	}

	log.Printf("Opening badger store at %s", loc)

	return badger.New(loc)
}

// initSQL opens the index at the --index location: a directory (with or
// without a sqlite:// or file:// scheme) holds an SQLite database and
// postgres:// names a database server.
func initSQL(u *url.URL, c *cli.Context, store backup.ObjectStore) (backup.Index, error) {
	location := u.String()

	switch u.Scheme {
	case "", "file", "sqlite":
		loc, err := makeLocation(u)
		if err != nil {
			return nil, err
		}

		location = loc
	}

	log.Printf("Opening %s index at %s", indexDialect(u.Scheme), location)

	// a local index keeps every commit until a policy is set
	x := sql.New(location, store)
	x.DefaultPolicy = ptr(retention.KeepAll)

	return x, nil
}

func indexDialect(scheme string) string {
	switch scheme {
	case "postgres", "postgresql":
		return "postgres"
	default:
		return "sqlite"
	}
}

var storageDrivers = map[string]func(*url.URL, *cli.Context) (backup.ObjectStore, error){
	"":       initPack,
	"file":   initSimple,
	"gcs":    initGCS,
	"goback": initRemote,
	"badger": initBadger,
}

var indexDrivers = map[string]func(*url.URL, *cli.Context, backup.ObjectStore) (backup.Index, error){
	"":           initSQL,
	"file":       initSQL,
	"sqlite":     initSQL,
	"postgres":   initSQL,
	"postgresql": initSQL,
}

// RestoreSession opens a session naming the object a restore reads, when
// the store has sessions, so retirement and garbage collection keep it
// while the restore runs. The returned func ends the session.
func RestoreSession(ctx context.Context, store backup.ObjectStore, set string, ref *proto.Ref) (context.Context, func(), error) {
	sessions, ok := store.(backup.SessionStore)
	if !ok {
		return ctx, func() {}, nil
	}

	sctx, err := sessions.BeginSession(ctx, &backup.Session{Set: set, Restore: ref})
	if err != nil {
		return nil, nil, err
	}

	return sctx, func() {
		if err := sessions.EndSession(sctx); err != nil {
			log.Printf("Failed ending restore session: %v", err)
		}
	}, nil
}

func GetObjectStore(c *cli.Context) backup.ObjectStore {
	location := c.GlobalString("storage")
	u, err := url.Parse(location)

	if err != nil {
		log.Fatalf("Invalid storage location %s: %v", location, err)
	}

	if driver, ok := storageDrivers[u.Scheme]; ok {
		store, err := driver(u, c)
		if err != nil {
			log.Fatalf("Could not initialise storage driver %s: %v", u.Scheme, err)
		}

		if op, ok := store.(Opener); ok {
			err := op.Open()
			if err != nil {
				log.Fatalf("Could not open object store %s: %v", u.Scheme, err)
			}
		}

		return store
	}

	log.Fatalf("No driver for storage location %s", u.String())
	return nil
}

// OpenIndex opens the index the --index location names, or the store
// itself when it is one.
func OpenIndex(c *cli.Context, store backup.ObjectStore) backup.Index {
	if idx, ok := store.(backup.Index); ok {
		log.Println("Store implements index")
		return idx
	}

	location := c.GlobalString("index")
	u, err := url.Parse(location)

	if err != nil {
		log.Fatalf("Invalid index location %s: %v", location, err)
	}

	log.Printf("Loading %s index driver", u.Scheme)

	if driver, ok := indexDrivers[u.Scheme]; ok {
		idx, err := driver(u, c, store)
		if err != nil {
			log.Fatalf("Could not initialise index driver %s: %v", u.Scheme, err)
		}

		err = idx.Open()
		if err != nil {
			log.Fatalf("Could not open object index %s: %v", u.Scheme, err)
		}

		return idx
	}

	log.Fatalf("No driver for storage location %s", u.String())
	return nil
}

// GetIndex opens the index for a command run without a server.

func ptr[T any](v T) *T {
	return &v
}
