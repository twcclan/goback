package packtest

import (
	"math/rand"
	"testing"

	"github.com/gobackio/goback/proto"
	"github.com/gobackio/goback/storage/pack"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
)

// TestArchive is a random archive name and index for exercising an
// ArchiveIndex.
type TestArchive struct {
	name  string
	index pack.IndexFile
}

// RandomIndexFile makes records with random sums, unsorted.
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

// RandomArchive makes a TestArchive with numRecords random records.
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
		archives[i] = RandomArchive(2345)
	}

	return archives
}

// TestArchiveIndex exercises indexing, lookup and deletion on idx.
func TestArchiveIndex(t *testing.T, idx pack.ArchiveIndex) {
	archives := getTestArchives(10)

	for _, archive := range archives {
		err := idx.IndexArchive(pack.ArchiveInfo{Name: archive.name}, archive.index)
		if err != nil {
			t.Fatalf("failed indexing test archive: %s", err)
		}
	}

	for _, archive := range archives {
		for _, i := range rand.Perm(len(archive.index)) {
			location, err := idx.LocateObject(&proto.Ref{Hash: archive.index[i].Sum[:]}, pack.Scope{})
			if err != nil {
				t.Errorf("couldn't find expected index record: %s", err)
				continue
			}

			if !cmp.Equal(location.Archive, archive.name) {
				t.Errorf("archive name mismatch: %s != %s", location.Archive, archive.name)
			}

			if !cmp.Equal(location.Record, archive.index[i]) {
				t.Error(cmp.Diff(location.Record, archive.index[i]))
			}
		}
	}

	gone := func(archives []TestArchive, want bool) {
		for _, archive := range archives {
			for _, record := range archive.index {
				_, err := idx.LocateObject(&proto.Ref{Hash: record.Sum[:]}, pack.Scope{})
				if found := err != pack.ErrRecordNotFound; found == want {
					t.Errorf("record %x of %s found: %v, %s", record.Sum, archive.name, found, err)
				}
			}
		}
	}

	if err := idx.DeleteArchives([]string{archives[0].name, archives[1].name, archives[2].name}); err != nil {
		t.Fatalf("Couldn't delete archives from index: %s", err)
	}

	gone(archives[:3], true)
	gone(archives[3:], false)

	for _, archive := range archives[3:] {
		if err := idx.DeleteArchives([]string{archive.name}); err != nil {
			t.Fatalf("Couldn't delete archive from index: %s", err)
		}
	}

	gone(archives, true)
}

// TestArchiveIndexExclusion checks that LocateObject skips excluded archives.
func TestArchiveIndexExclusion(t *testing.T, idx pack.ArchiveIndex) {
	archives := getTestArchives(10)

	// the second half duplicates the first
	for i := 0; i < len(archives)/2; i++ {
		to := i + len(archives)/2
		from := i

		archives[to].index = archives[from].index
	}

	for _, archive := range archives {
		err := idx.IndexArchive(pack.ArchiveInfo{Name: archive.name}, archive.index)
		if err != nil {
			t.Fatalf("failed indexing test archive: %s", err)
		}
	}

	for _, archive := range archives[:len(archives)/2] {
		for _, i := range rand.Perm(len(archive.index)) {
			record := archive.index[i].Sum[:]

			location1, err := idx.LocateObject(&proto.Ref{Hash: record}, pack.Scope{})
			if err != nil {
				t.Errorf("couldn't find expected index record: %s", err)
				continue
			}

			location2, err := idx.LocateObject(&proto.Ref{Hash: record}, pack.Scope{}, archive.name)
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

// BenchmarkLookup times LocateObject over ten indexed archives.
func BenchmarkLookup(b *testing.B, idx pack.ArchiveIndex) {
	archives := getTestArchives(10)

	for _, archive := range archives {
		err := idx.IndexArchive(pack.ArchiveInfo{Name: archive.name}, archive.index)
		if err != nil {
			b.Fatalf("failed indexing test archive: %s", err)
		}
	}

	b.Run("lookups", func(b *testing.B) {
		lookups := make([]*proto.Ref, b.N)
		for i := range lookups {
			randomArchive := archives[rand.Intn(len(archives))]
			randomObject := randomArchive.index[rand.Intn(len(randomArchive.index))].Sum[:]

			lookups[i] = &proto.Ref{Hash: randomObject}
		}

		b.ResetTimer()

		for _, ref := range lookups {
			_, err := idx.LocateObject(ref, pack.Scope{})
			if err != nil {
				b.Errorf("couldn't find expected index record: %s", err)
				continue
			}
		}
	})
}

// BenchmarkIndex times IndexArchive on an archive of b.N records.
func BenchmarkIndex(b *testing.B, idx pack.ArchiveIndex) {
	archive := RandomArchive(b.N)

	b.ResetTimer()

	err := idx.IndexArchive(pack.ArchiveInfo{Name: archive.name}, archive.index)
	if err != nil {
		b.Fatal(err)
	}
}

// Name is the archive's name.
func (a TestArchive) Name() string { return a.name }

// Index is the archive's index file.
func (a TestArchive) Index() pack.IndexFile { return a.index }
