package packtest

import (
	"log"
	"math/rand"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/twcclan/goback/proto"

	"github.com/twcclan/goback/storage/pack"

	"github.com/google/uuid"
)

type TestArchive struct {
	name  string
	index pack.IndexFile
}

func RandomIndexFile(records int) pack.IndexFile {
	idx := make(pack.IndexFile, records)

	for i := range idx {
		idx[i].Offset = rand.Uint32()
		idx[i].Length = rand.Uint32()
		idx[i].Type = rand.Uint32()
		for j := range idx[i].Sum {
			idx[i].Sum[j] = byte(rand.Intn(256))
		}
	}

	return idx
}

func RandomArchive(numRecords int) TestArchive {
	archive := TestArchive{
		name:  uuid.New().String(),
		index: RandomIndexFile(numRecords),
	}

	return archive
}

func getTestArchives(num int) []TestArchive {
	archives := make([]TestArchive, num)
	for i := range archives {
		archives[i] = RandomArchive(1000)
	}

	return archives
}

func TestArchiveIndex(t *testing.T, idx pack.ArchiveIndex) {
	archives := getTestArchives(10)

	for i, archive := range archives {
		log.Printf("indexing test archive %d/%d", i+1, len(archives))
		err := idx.Index(archive.name, archive.index)
		if err != nil {
			t.Fatalf("failed indexing test archive: %s", err)
		}
	}

	for i, archive := range archives {
		log.Printf("searching test archive %d/%d", i+1, 128)
		for _, i := range rand.Perm(len(archive.index)) {
			location, err := idx.Locate(&proto.Ref{Sha1: archive.index[i].Sum[:]})
			if err != nil {
				t.Errorf("couldn't find expected index record: %s", err)
				continue
			}

			if !cmp.Equal(location.Archive, archive.name) {
				t.Errorf("archive name mismatch: %s != %s", location.Archive, archive.name)
			}

			if !cmp.Equal(location.Record, archive.index[i]) {
				t.Errorf(cmp.Diff(location.Record, archive.index[i]))
			}
		}
	}

	for i, archive := range archives {
		log.Printf("deleting test archive %d/%d", i+1, 128)
		err := idx.Delete(archive.name, archive.index)
		if err != nil {
			t.Errorf("Couldn't delete archive from index: %s", err)
			continue
		}

		for _, record := range archive.index {
			_, err := idx.Locate(&proto.Ref{Sha1: record.Sum[:]})
			if err != pack.ErrRecordNotFound {
				t.Errorf("Expected to not find index record for key %x: %s", record.Sum, err)
			}
		}
	}
}

func TestArchiveIndexExclusion(t *testing.T, idx pack.ArchiveIndex) {
	archives := getTestArchives(10)

	// replace the second half of the archives with a copy of the first
	for i := 0; i < len(archives)/2; i++ {
		to := i + len(archives)/2
		from := i

		archives[to].index = archives[from].index
	}

	for _, archive := range archives {
		err := idx.Index(archive.name, archive.index)
		if err != nil {
			t.Fatalf("failed indexing test archive: %s", err)
		}
	}

	for _, archive := range archives[:len(archives)/2] {
		for _, i := range rand.Perm(len(archive.index)) {
			record := archive.index[i].Sum[:]

			location1, err := idx.Locate(&proto.Ref{Sha1: record})
			if err != nil {
				t.Errorf("couldn't find expected index record: %s", err)
				continue
			}

			location2, err := idx.Locate(&proto.Ref{Sha1: record}, archive.name)
			if err != nil {
				t.Errorf("couldn't find expected index record: %s", err)
				continue
			}

			if cmp.Equal(location1.Archive, location2.Archive) {
				t.Errorf("Expected to find two different archives for record: %x", record)
			}
		}
	}
}

func BenchmarkLookup(b *testing.B, idx pack.ArchiveIndex) {
	archives := getTestArchives(10)

	for _, archive := range archives {
		err := idx.Index(archive.name, archive.index)
		if err != nil {
			b.Fatalf("failed indexing test archive: %s", err)
		}
	}

	b.Run("lookups", func(b *testing.B) {
		lookups := make([]*proto.Ref, b.N)
		for i := range lookups {
			randomArchive := archives[rand.Intn(len(archives))]
			randomObject := randomArchive.index[rand.Intn(len(randomArchive.index))].Sum[:]

			lookups[i] = &proto.Ref{Sha1: randomObject}
		}

		b.ResetTimer()

		for _, ref := range lookups {
			_, err := idx.Locate(ref)
			if err != nil {
				b.Errorf("couldn't find expected index record: %s", err)
				continue
			}
		}
	})
}

func BenchmarkIndex(b *testing.B, idx pack.ArchiveIndex) {
	archive := RandomArchive(b.N)

	b.ResetTimer()

	err := idx.Index(archive.name, archive.index)
	if err != nil {
		b.Fatal(err)
	}
}
