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
	"github.com/twcclan/goback/storage/pack"
	"github.com/twcclan/goback/storage/wrapped"

	"github.com/urfave/cli"
	"gocloud.dev/blob"
	"gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/gcsblob"
)

// Opener is implemented by stores that must be opened before use.
type Opener interface {
	Open() error
}

// Closer is implemented by stores and indexes that must be closed.
type Closer interface {
	Close() error
}

// CloseStore compacts a store that can and closes it.
func CloseStore(store backup.ObjectStore) {
	if c, ok := store.(interface{ Compact() error }); ok {
		if err := c.Compact(); err != nil {
			log.Printf("Compaction failed: %v", err)
		}
	}

	if cl, ok := store.(Closer); ok {
		if err := cl.Close(); err != nil {
			log.Printf("Closing the store failed: %v", err)
		}
	}
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
	for inner := wrapped.Unwrap(store); inner != nil; inner = wrapped.Unwrap(store) {
		store = inner
	}

	return store
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
		pack.WithCompaction(pack.CompactionConfig{MinimumCandidates: 100}),
	)...)
}

// atRest is the pack options for the global --at-rest-key and
// --retired-at-rest-key flags, none without them.
func atRest(c *cli.Context) ([]pack.PackOption, error) {
	var options []pack.PackOption

	if path := c.GlobalString("at-rest-key"); path != "" {
		key, err := storekey.Load(path)
		if err != nil {
			return nil, fmt.Errorf("loading the at-rest key: %w", err)
		}

		options = append(options, pack.WithAtRestKey(key))
	}

	for _, path := range c.GlobalStringSlice("retired-at-rest-key") {
		key, err := storekey.Load(path)
		if err != nil {
			return nil, fmt.Errorf("loading the retired at-rest key %s: %w", path, err)
		}

		options = append(options, pack.WithRetiredAtRestKey(key))
	}

	return options, nil
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

	key, err := APIKey(c)
	if err != nil {
		return nil, err
	}

	return storage.NewClient(addr, auth.Credentials{Secret: key, AgentID: AgentID(c)}, tlsConfig)
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
	"gcs":    initGCS,
	"goback": initRemote,
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

// RechunkFlag turns --rechunk on a restore command.
var RechunkFlag = cli.BoolFlag{
	Name:  "rechunk",
	Usage: "cut an existing destination with the backup's chunker, so parts that only moved are not downloaded",
}

// SalvageFlag turns --salvage on a restore command.
var SalvageFlag = cli.BoolFlag{
	Name:  "salvage",
	Usage: "write what can be read when parts are missing, leaving holes, and report them",
}

// Salvage applies --salvage to the restorer and logs every hole it leaves.
func Salvage(c *cli.Context, restorer *backup.Restorer) {
	if !c.Bool("salvage") {
		return
	}

	restorer.Salvage = true
	restorer.OnHole = func(hole backup.Hole) {
		log.Printf("missing %d bytes at offset %d of %s (%x)", hole.Length, hole.Offset, hole.Path, hole.Ref.GetHash())
	}
}

// GetObjectStore opens the store the global --storage location names, or
// exits.
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

func ptr[T any](v T) *T {
	return &v
}
