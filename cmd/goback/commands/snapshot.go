package commands

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/bmatcuk/doublestar"
	"github.com/codegangsta/cli"
	"github.com/dustin/go-humanize"
)

var Snapshot = cli.Command{
	Name:   "snapshot",
	Action: snapshotCmd,
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

type snapshot struct {
	store    backup.ChunkStore
	index    backup.Index
	base     string
	includes []string
	excludes []string
}

func (s *snapshot) storeChunk(chunk *proto.Chunk) error {
	// don't store chunks that we know exist already
	found, err := s.index.HasChunk(chunk.Ref)
	if err != nil || found {
		return err
	}

	err = s.store.Create(chunk)
	if err != nil {
		return err
	}

	return s.index.Index(chunk)
}

func (s *snapshot) readFile(fName string, fInfo os.FileInfo) (*proto.ChunkRef, error) {
	infoRef, fileInfo, err := s.index.Stat(fName)
	if err != nil {
		return nil, err
	}

	// exit early if the file hasn't changed
	if fileInfo != nil && fileInfo.Timestamp >= fInfo.ModTime().UTC().Unix() {
		return infoRef, nil
	}

	log.Printf("File modified (stored: %s modified: %s): %s %s",
		humanize.Time(time.Unix(fileInfo.Timestamp, 0)), humanize.Time(fInfo.ModTime().UTC()), fName, humanize.Bytes(uint64(fInfo.Size())))

	// open input file for reading
	file, err := os.Open(fName)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// split the file and store all the chunks
	chunkRefs := make([]*proto.ChunkRef, 0)
	for chunk := range backup.Split(file) {
		err = s.storeChunk(chunk)
		if err != nil {
			return nil, err
		}

		chunkRefs = append(chunkRefs, &proto.ChunkRef{Sum: chunk.Ref.Sum})
	}

	// store the file -> chunks mapping
	fileDataChunk := proto.GetMetaChunk(&proto.File{
		Size:   fInfo.Size(),
		Chunks: chunkRefs,
	}, proto.ChunkType_FILE_DATA)

	if err := s.storeChunk(fileDataChunk); err != nil {
		return nil, err
	}

	// store the file info -> file mapping
	info := proto.GetFileInfo(fInfo)
	info.Name = fName
	info.Data = proto.GetUntypedRef(fileDataChunk.Ref)

	infoChunk := proto.GetMetaChunk(info, proto.ChunkType_FILE_INFO)
	if err := s.storeChunk(infoChunk); err != nil {
		return nil, err
	}

	// return file info ref for inclusion in snapshot
	return infoChunk.Ref, nil
}

func (s *snapshot) hasChanged() bool {
	return true
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
	files := make([]*proto.ChunkRef, 0)

	err := filepath.Walk(s.base, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			log.Print(err)
			if info != nil && info.IsDir() {
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

		infoRef, err := s.readFile(p, info)
		if err != nil {
			return err
		}

		files = append(files, proto.GetUntypedRef(infoRef))

		return nil
	})

	if err != nil {
		log.Fatal(err)
	}

	err = s.storeChunk(proto.GetMetaChunk(&proto.Snapshot{
		Files:     files,
		Timestamp: time.Now().UTC().Unix(),
	}, proto.ChunkType_SNAPSHOT))

	if err != nil {
		log.Fatal(err)
	}
}

func snapshotCmd(c *cli.Context) {
	base := "."
	if c.Args().Present() {
		base = c.Args().First()
	}

	idx := getIndex(c)

	err := idx.Open()
	if err != nil {
		log.Fatal(err)
	}

	store := backup.NewNopStorage()

	s := &snapshot{
		index:    idx,
		store:    store,
		base:     base,
		includes: c.StringSlice("include"),
		excludes: c.StringSlice("exclude"),
	}

	s.take()

	err = idx.Close()
	if err != nil {
		log.Fatal(err)
	}

	store.Stats()
}
