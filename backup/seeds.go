package backup

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/twcclan/goback/backup/chunker"
	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/proto"
)

// SeedMap locates chunks of user-named files by the ref they would have
// been stored under, so a restore can take them from disk.
type SeedMap struct {
	key     *storekey.Key
	entries map[string]seedEntry
}

type seedEntry struct {
	path   string
	offset int64
	length int64
}

// NewSeedMap returns an empty map; key is the store key the files were
// backed up under, nil for a plaintext store.
func NewSeedMap(key *storekey.Key) *SeedMap {
	return &SeedMap{key: key, entries: make(map[string]seedEntry)}
}

// Len is the number of chunks the map locates.
func (s *SeedMap) Len() int { return len(s.entries) }

// Add indexes path, or every regular file under it, cut with the chunker
// the backup uses.
func (s *SeedMap) Add(path string) error {
	return filepath.WalkDir(path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.Type().IsRegular() {
			return nil
		}

		return s.addFile(path)
	})
}

func (s *SeedMap) addFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	var cdc chunker.FastCDC
	buf := make([]byte, chunker.MaxSize)
	filled, scanned := 0, 0
	var offset int64
	eof := false

	for {
		if !eof && filled < len(buf) {
			n, err := io.ReadFull(file, buf[filled:])
			filled += n

			switch err {
			case nil:
			case io.EOF, io.ErrUnexpectedEOF:
				eof = true
			default:
				return err
			}
		}

		if filled == 0 {
			return nil
		}

		cut := cdc.Scan(buf[:filled], scanned)
		if cut == 0 {
			if !eof {
				scanned = filled
				continue
			}

			cut = filled
			cdc.Reset()
		}

		s.record(path, offset, buf[:cut], info.Size())

		offset += int64(cut)
		copy(buf, buf[cut:filled])
		filled -= cut
		scanned = 0
	}
}

func (s *SeedMap) record(path string, offset int64, chunk []byte, fileSize int64) {
	var ref *proto.Ref

	if s.key != nil {
		if mode := s.key.Choose(fileSize, chunk); mode != proto.Encryption_PLAINTEXT {
			ref = storekey.RefOf(s.key.BlobKey(mode, chunk))
		}
	}

	if ref == nil {
		ref = proto.NewObject(&proto.Blob{Data: chunk}).Ref()
	}

	key := string(ref.Hash)
	if _, seen := s.entries[key]; !seen {
		s.entries[key] = seedEntry{path: path, offset: offset, length: int64(len(chunk))}
	}
}

// read returns the bytes recorded for the part's ref, unverified.
func (s *SeedMap) read(part *proto.FilePart) ([]byte, bool) {
	entry, ok := s.entries[string(part.Ref.Hash)]
	if !ok || uint64(entry.length) != part.Length {
		return nil, false
	}

	file, err := os.Open(entry.path)
	if err != nil {
		return nil, false
	}
	defer file.Close()

	buf := make([]byte, entry.length)
	if _, err := file.ReadAt(buf, entry.offset); err != nil {
		return nil, false
	}

	return buf, true
}
