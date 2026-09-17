package pack

import (
	"errors"
	"sync"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
)

// NewInMemoryIndex returns an empty index.
func NewInMemoryIndex() *InMemoryIndex {
	return &InMemoryIndex{
		index:    make(map[string]map[[proto.HashSize]byte]IndexRecord),
		archives: make(map[string]ArchiveInfo),
		sessions: make(map[string]*backup.Session),
	}
}

var _ ArchiveIndex = (*InMemoryIndex)(nil)

// InMemoryIndex is an ArchiveIndex kept entirely in memory, for tests and
// short-lived tools.
type InMemoryIndex struct {
	mtx      sync.RWMutex
	index    map[string]map[[proto.HashSize]byte]IndexRecord
	archives map[string]ArchiveInfo
	sessions map[string]*backup.Session
}

// LocateObject implements pack.ArchiveIndex.
func (i *InMemoryIndex) LocateObject(ref *proto.Ref, scope Scope, exclude ...string) (IndexLocation, error) {
	var sum [proto.HashSize]byte
	copy(sum[:], ref.Hash)

	i.mtx.RLock()
	defer i.mtx.RUnlock()

outer:
	for archive, records := range i.index {
		for _, excluded := range exclude {
			if excluded == archive {
				continue outer
			}
		}

		if !scope.Visible(i.archives[archive]) {
			continue
		}

		if record, ok := records[sum]; ok {
			return IndexLocation{
				Archive: archive,
				Record:  record,
			}, nil
		}
	}

	return IndexLocation{}, ErrRecordNotFound
}

// LookupArchive implements pack.ArchiveIndex.
func (i *InMemoryIndex) LookupArchive(archive string) (ArchiveInfo, bool, error) {
	i.mtx.RLock()
	defer i.mtx.RUnlock()

	info, ok := i.archives[archive]

	return info, ok, nil
}

// IndexArchive implements pack.ArchiveIndex.
func (i *InMemoryIndex) IndexArchive(archive ArchiveInfo, index IndexFile) error {
	a := make(map[[proto.HashSize]byte]IndexRecord)

	for _, record := range index {
		a[record.Sum] = record
	}

	i.mtx.Lock()
	defer i.mtx.Unlock()

	if _, ok := i.archives[archive.Name]; ok {
		return nil
	}

	if archive.State == ArchivePending {
		if _, ok := i.sessions[archive.Session]; !ok {
			return backup.ErrNoSession
		}
	}

	i.index[archive.Name] = a
	i.archives[archive.Name] = archive

	return nil
}

// DeleteArchive implements pack.ArchiveIndex.
func (i *InMemoryIndex) DeleteArchive(archive string, index IndexFile) error {
	i.mtx.Lock()
	delete(i.index, archive)
	delete(i.archives, archive)
	i.mtx.Unlock()

	return nil
}

// BeginSession implements SessionIndex.
func (i *InMemoryIndex) BeginSession(s *backup.Session) error {
	i.mtx.Lock()
	defer i.mtx.Unlock()

	if _, ok := i.sessions[s.ID]; ok {
		return errors.New("session already exists")
	}

	copied := *s
	i.sessions[s.ID] = &copied

	return nil
}

// TouchSession implements SessionIndex.
func (i *InMemoryIndex) TouchSession(id string, at time.Time) error {
	i.mtx.Lock()
	defer i.mtx.Unlock()

	s, ok := i.sessions[id]
	if !ok {
		return backup.ErrNoSession
	}

	s.LastSeen = at

	return nil
}

// GetSession implements SessionIndex.
func (i *InMemoryIndex) GetSession(id string) (*backup.Session, error) {
	i.mtx.RLock()
	defer i.mtx.RUnlock()

	s, ok := i.sessions[id]
	if !ok {
		return nil, backup.ErrNoSession
	}

	copied := *s

	return &copied, nil
}

// ListSessions implements SessionIndex.
func (i *InMemoryIndex) ListSessions() ([]*backup.Session, error) {
	i.mtx.RLock()
	defer i.mtx.RUnlock()

	sessions := make([]*backup.Session, 0, len(i.sessions))
	for _, s := range i.sessions {
		copied := *s
		sessions = append(sessions, &copied)
	}

	return sessions, nil
}

// EndSession implements SessionIndex.
func (i *InMemoryIndex) EndSession(id string) ([]string, error) {
	i.mtx.Lock()
	defer i.mtx.Unlock()

	var dropped []string
	for name, info := range i.archives {
		if info.State == ArchivePending && info.Session == id {
			dropped = append(dropped, name)
			delete(i.archives, name)
			delete(i.index, name)
		}
	}

	delete(i.sessions, id)

	return dropped, nil
}

// CommitSession implements SessionIndex.
func (i *InMemoryIndex) CommitSession(id string) error {
	i.mtx.Lock()
	defer i.mtx.Unlock()

	if _, ok := i.sessions[id]; !ok {
		return backup.ErrNoSession
	}

	for name, info := range i.archives {
		if info.State == ArchivePending && info.Session == id {
			info.State = ArchiveCommitted
			info.Session = ""
			i.archives[name] = info
		}
	}

	return nil
}

// Close implements io.Closer.
func (i *InMemoryIndex) Close() error {
	i.mtx.Lock()
	i.index = make(map[string]map[[proto.HashSize]byte]IndexRecord)
	i.archives = make(map[string]ArchiveInfo)
	i.sessions = make(map[string]*backup.Session)
	i.mtx.Unlock()

	return nil
}

// CountObjects reports the indexed and the distinct object count.
func (i *InMemoryIndex) CountObjects() (uint64, uint64, error) {
	return 0, 0, errors.New("not implemented")
}
