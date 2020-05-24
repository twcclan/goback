package common

import (
	"log"
	"net"
	"net/url"
	"os"

	"github.com/twcclan/goback/backup"
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

func initSwift(u *url.URL, c *cli.Context) (backup.ObjectStore, error) {
	return storage.NewSwiftObjectStore(
		"teUHDFyfMNqU",
		"mppPgm529A6MNx6UNFqTxvnYyMv7Wqsy",
		"https://auth.cloud.ovh.net/v2.0",
		"1675837378358194",
		"goback-test",
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

var storageDrivers = map[string]func(*url.URL, *cli.Context) (backup.ObjectStore, error){
	"":       initPack,
	"file":   initSimple,
	"swift":  initSwift,
	"gcs":    initGCS,
	"goback": initRemote,
}

func getIndexLocation(c *cli.Context) (string, error) {
	return makeLocation(c.GlobalString("index"))
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
	loc, err := getIndexLocation(c)
	if err != nil {
		log.Fatalf("Could not initialise index driver: %v", err)
	}
	return sqlite.NewSqliteIndex(loc, c.GlobalString("set"), store)
}
