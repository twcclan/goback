package common

import (
	"log"
	"net"
	"net/url"
	"os"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/index/postgres"
	"github.com/twcclan/goback/index/sqlite"
	"github.com/twcclan/goback/storage"
	"github.com/twcclan/goback/storage/pack"

	"github.com/urfave/cli"
)

type Opener interface {
	Open() error
}

type Closer interface {
	Close() error
}

func makeLocation(loc string) (string, error) {
	err := os.MkdirAll(loc, os.FileMode(0700))
	if err != nil {
		if os.IsExist(err) {
			return "", err
		}
	}

	return loc, nil
}

func initSimple(u *url.URL, c *cli.Context) (backup.ObjectStore, error) {
	loc, err := makeLocation(u.Path)

	return storage.NewSimpleObjectStore(loc), err
}

func initPack(u *url.URL, c *cli.Context) (backup.ObjectStore, error) {
	loc, err := makeLocation(u.Path)
	if err != nil {
		return nil, err
	}

	return pack.NewPackStorage(
		pack.WithArchiveStorage(pack.NewLocalArchiveStorage(loc)),
		pack.WithMaxParallel(1),
		pack.WithMaxSize(1024*1024*1024),
		pack.WithCompaction(pack.CompactionConfig{
			OnClose:           true,
			MinimumCandidates: 100,
		}),
	)
}

func initGCS(u *url.URL, c *cli.Context) (backup.ObjectStore, error) {
	return storage.NewGCSObjectStore(u.Host, u.Query().Get("cache"))
}

func initRemote(u *url.URL, c *cli.Context) (backup.ObjectStore, error) {
	port := u.Port()
	if port == "" {
		port = "6060"
	}

	addr := net.JoinHostPort(u.Host, port)

	return storage.NewRemoteClient(addr)
}

func initSqlite(u *url.URL, c *cli.Context, store backup.ObjectStore) (backup.Index, error) {
	loc, err := makeLocation(u.Path)
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

var storageDrivers = map[string]func(*url.URL, *cli.Context) (backup.ObjectStore, error){
	"":       initPack,
	"file":   initSimple,
	"gcs":    initGCS,
	"goback": initRemote,
}

var indexDrivers = map[string]func(*url.URL, *cli.Context, backup.ObjectStore) (backup.Index, error){
	"":         initSqlite,
	"sqlite":   initSqlite,
	"postgres": initPostgres,
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
