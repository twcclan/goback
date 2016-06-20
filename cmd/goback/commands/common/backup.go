package common

import (
	"errors"
	"log"
	"net/url"
	"os"
	"path/filepath"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/storage"

	"github.com/codegangsta/cli"
	"github.com/goamz/goamz/aws"
)

func initS3(u *url.URL, c *cli.Context) (backup.ObjectStore, error) {
	auth, err := aws.EnvAuth()

	if err != nil {
		return nil, err
	}

	region, ok := aws.Regions[u.Host]
	if !ok {
		return nil, errors.New("No or invalid AWS region specified")
	}

	log.Println(filepath.Base(u.Path))

	return storage.NewS3ChunkStore(auth, region, filepath.Base(u.Path)), nil
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

var storageDrivers = map[string]func(*url.URL, *cli.Context) (backup.ObjectStore, error){
	"":     initSimple,
	"file": initSimple,
	"s3":   initS3,
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

		return store
	}

	log.Fatalf("No driver for storage location %s", u.String())
	return nil
}

/*
func GetIndex(c *cli.Context) backup.Index {
	loc, err := getIndexLocation(c)
	if err != nil {
		log.Fatalf("Could not initialise index driver: %v", err)
	}
	return backup.NewSqliteIndex(loc)
}
*/
