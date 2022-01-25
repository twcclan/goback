package common

import (
	"errors"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"

	"github.com/twcclan/goback/backup"
	badgerIdx "github.com/twcclan/goback/index/badger"
	"github.com/twcclan/goback/index/crdb"
	"github.com/twcclan/goback/index/postgres"
	"github.com/twcclan/goback/index/sqlite"
	"github.com/twcclan/goback/storage"
	"github.com/twcclan/goback/storage/badger"
	"github.com/twcclan/goback/storage/pack"

	"github.com/urfave/cli"
)

type Opener interface {
	Open() error
}

type Closer interface {
	Close() error
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

	return pack.New(
		pack.WithArchiveStorage(storage.NewLocalArchiveStorage(archiveLocation)),
		pack.WithArchiveIndex(idx),
		pack.WithMaxParallel(1),
		pack.WithMaxSize(1024*1024*1024),
		pack.WithCompaction(pack.CompactionConfig{
			OnClose:           true,
			MinimumCandidates: 100,
		}),
	)
}

func initGCS(u *url.URL, c *cli.Context) (backup.ObjectStore, error) {
	return storage.NewGCSObjectStore(u.Host, u.Query().Get("index"), u.Query().Get("cache"))
}

func initRemote(u *url.URL, c *cli.Context) (backup.ObjectStore, error) {
	port := u.Port()
	if port == "" {
		port = "6060"
	}

	addr := net.JoinHostPort(u.Host, port)

	return storage.NewRemoteClient(addr)
}

func initBadger(u *url.URL, c *cli.Context) (backup.ObjectStore, error) {
	loc, err := makeLocation(u)
	if err != nil {
		return nil, err
	}

	log.Printf("Opening badger store at %s", loc)

	return badger.New(loc)
}

func initSqlite(u *url.URL, c *cli.Context, store backup.ObjectStore) (backup.Index, error) {
	loc, err := makeLocation(u)
	if err != nil {
		return nil, err
	}
	log.Printf("Opening sqlite index at %s", loc)

	return sqlite.NewIndex(loc, c.GlobalString("set"), store), nil
}

func initPostgres(u *url.URL, c *cli.Context, store backup.ObjectStore) (backup.Index, error) {
	log.Printf("Opening postgres index")
	return postgres.NewIndex(u.String(), store), nil
}

func initCrdb(u *url.URL, c *cli.Context, store backup.ObjectStore) (backup.Index, error) {
	log.Printf("Opening postgres index")
	return postgres.NewIndex(u.String(), store), nil
}

func initServerless(u *url.URL, context *cli.Context) (backup.ObjectStore, error) {
	database := u.Query().Get("database")

	if database == "" {
		return nil, errors.New("database url must be provided")
	}

	index := crdb.New(database)

	return storage.NewGCSObjectStore(u.Host, u.Query().Get("index"), u.Query().Get("cache"))
}

var storageDrivers = map[string]func(*url.URL, *cli.Context) (backup.ObjectStore, error){
	"":           initPack,
	"file":       initSimple,
	"gcs":        initGCS,
	"goback":     initRemote,
	"badger":     initBadger,
	"serverless": initServerless,
}
var indexDrivers = map[string]func(*url.URL, *cli.Context, backup.ObjectStore) (backup.Index, error){
	"":         initSqlite,
	"sqlite":   initSqlite,
	"postgres": initPostgres,
	"crdb":     initCrdb,
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

func GetIndex(c *cli.Context, store backup.ObjectStore) backup.Index {
	// if the provided store already implements index just return it
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
