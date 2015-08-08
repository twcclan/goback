package snapshot

import (
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/cmd/goback/commands/common"

	"github.com/bmatcuk/doublestar"
	"github.com/codegangsta/cli"
)

func (s *snapshot) readFile(fName string, fInfo os.FileInfo) error {
	writer, err := s.backup.Create(fName, fInfo)
	if err != nil {
		if err == os.ErrExist {
			//file exists already, skip it
			return nil
		}

		return err
	}

	// open input file for reading
	file, err := os.Open(fName)

	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(writer, file)
	if err != nil {
		return err
	}

	return writer.Close()
}

func (s *snapshot) shouldInclude(fName string) bool {
	// check whitelist first
	for _, pat := range s.includes {
		match, err := doublestar.Match(pat, fName)
		if err != nil {
			log.Printf("Malformed pattern: \"%s\" %v", pat, err)
		}

		if match {
			//log.Printf("Whitelisted file: %s", fName)
			return true
		}
	}

	// check blacklist
	for _, pat := range s.excludes {
		match, err := doublestar.Match(pat, fName)
		if err != nil {
			log.Printf("Malformed pattern: \"%s\" %v", pat, err)
		}

		if match {
			//log.Printf("Blacklisted file: %s", fName)
			return false
		}
	}

	// default to allowing
	return true
}

func (s *snapshot) take() {
	err := filepath.Walk(s.base, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			log.Print(err)
			if info != nil && info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return filepath.SkipDir
			} else {
				return nil
			}
		}

		// skip files that we are not interested in
		// we also "skip" processing directories, but
		// still recurse into them
		if info.IsDir() || !s.shouldInclude(p) {
			return nil
		}

		err = s.readFile(p, info)
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		log.Fatal(err)
	}

	err = s.backup.Close()
	if err != nil {
		log.Fatal(err)
	}
}

func newAction(c *cli.Context) {
	base := "."
	if c.Args().Present() {
		base = c.Args().First()
	}

	idx := common.GetIndex(c)

	err := idx.Open()
	if err != nil {
		log.Fatal(err)
	}

	store := common.GetChunkStore(c)

	s := &snapshot{
		backup:   backup.NewBackupWriter(idx, store),
		reader:   backup.NewBackupReader(idx, store),
		base:     base,
		includes: c.StringSlice("include"),
		excludes: c.StringSlice("exclude"),
	}

	s.take()

	err = idx.Close()
	if err != nil {
		log.Fatal(err)
	}
}

var newCmd = cli.Command{
	Name:        "new",
	Description: "Take a snapshot",
	Action:      newAction,
	Flags: []cli.Flag{
		cli.StringSliceFlag{
			Name:  "include, i",
			Value: new(cli.StringSlice),
		},
		cli.StringSliceFlag{
			Name:  "exclude, e",
			Value: new(cli.StringSlice),
		},
	},
}
