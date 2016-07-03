package commit

import (
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strings"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/cmd/goback/commands/common"

	"github.com/bmatcuk/doublestar"
	"github.com/codegangsta/cli"
	"github.com/pkg/errors"
)

func (s *commit) shouldInclude(fName string) bool {
	// check the patterns against paths relative to our base
	fName = strings.TrimPrefix(fName, s.base)

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

func (s *commit) read(file string) func(io.Writer) error {
	return func(writer io.Writer) error {
		reader, err := os.Open(file)
		if err != nil {
			return errors.Wrapf(err, "Failed opening %s for reading", file)
		}
		defer reader.Close()

		_, err = io.Copy(writer, reader)

		return errors.Wrapf(err, "Failed writing file %s", file)
	}
}

func (s *commit) descend(absPath string) func(backup.TreeWriter) error {
	return func(tree backup.TreeWriter) error {
		files, err := ioutil.ReadDir(absPath)
		if err != nil {
			return errors.Wrapf(err, "Failed opening %s for listing", absPath)
		}

		for _, file := range files {
			absPath = path.Join(absPath, file.Name())

			if !s.shouldInclude(absPath) {
				continue
			}

			if file.IsDir() {
				// recurse into the sub folder
				err = tree.Tree(file, s.descend(absPath))

			} else if file.Mode()&os.ModeSymlink == 0 { // skip symlinks
				// store the file
				err = tree.File(file, s.read(absPath))
			}

			if err != nil {
				return err
			}
		}

		return nil
	}
}

func (s *commit) take() {
	err := s.descend(s.base)(s.backup)

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

	store := common.GetObjectStore(c)

	s := &commit{
		backup:   backup.NewBackupWriter(store),
		base:     base,
		includes: c.StringSlice("include"),
		excludes: c.StringSlice("exclude"),
	}

	s.take()
}

var newCmd = cli.Command{
	Name:        "new",
	Description: "Create a new commit",
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
