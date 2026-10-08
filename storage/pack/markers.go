package pack

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gobackio/goback/backup"
	"github.com/gobackio/goback/proto"
)

// SessionBeginExt and SessionEndExt name a session's markers in the
// storage: <id>.begin holds the session as it began and <id>.end how it
// ended. An end is only ever created once, so a commit and anything ending
// the session agree on a single outcome.
const (
	SessionBeginExt = ".begin"
	SessionEndExt   = ".end"
)

type sessionOutcome string

const (
	sessionCommitted sessionOutcome = "committed"
	sessionAborted   sessionOutcome = "aborted"
)

type beginMarker struct {
	ID      string    `json:"id"`
	AgentID string    `json:"agent_id"`
	Set     string    `json:"set"`
	Restore []byte    `json:"restore,omitempty"`
	Started time.Time `json:"started"`
	// Sealed is the newest generation that had left a seal when the
	// session began.
	Sealed uint64 `json:"sealed,omitempty"`
}

func (ps *PackStorage) markBegun(s *backup.Session, sealed uint64) error {
	data, err := json.Marshal(beginMarker{ID: s.ID, AgentID: s.AgentID, Set: s.Set, Restore: s.Restore.GetHash(), Started: s.Started, Sealed: sealed})
	if err != nil {
		return err
	}

	err = ps.storage.CreateNew(s.ID+SessionBeginExt, data)
	if err != nil {
		return fmt.Errorf("marking session %s begun: %w", s.ID, err)
	}

	return nil
}

// markEnded ends the session with want unless it has ended already, and
// returns the outcome it has.
func (ps *PackStorage) markEnded(id string, want sessionOutcome) (sessionOutcome, error) {
	err := ps.storage.CreateNew(id+SessionEndExt, []byte(want))
	if err == nil {
		return want, nil
	}

	if !errors.Is(err, ErrFileExists) {
		return "", fmt.Errorf("marking session %s ended: %w", id, err)
	}

	file, err := ps.storage.Open(id + SessionEndExt)
	if err != nil {
		return "", err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}

	return sessionOutcome(data), nil
}

// reconcileSessions brings the index's sessions in line with the markers:
// an indexed session that has ended is ended, and one that began without
// ending is indexed again if the index lost it, so it expires like any
// other.
func (ps *PackStorage) reconcileSessions() error {
	begun, err := ps.markerIDs(SessionBeginExt)
	if err != nil {
		return err
	}

	ended, err := ps.markerIDs(SessionEndExt)
	if err != nil {
		return err
	}

	indexed, err := ps.index.ListSessions()
	if err != nil {
		return err
	}

	known := make(map[string]bool, len(indexed))
	for _, s := range indexed {
		known[s.ID] = true

		if ended[s.ID] {
			if err := ps.endSession(s.ID); err != nil {
				return err
			}
		}
	}

	for id := range begun {
		if ended[id] || known[id] {
			continue
		}

		s, err := ps.readBegin(id)
		if err != nil {
			return err
		}

		ps.logger.Info("indexing a session the index lost", "session", id)

		if err := ps.index.BeginSession(s); err != nil {
			return err
		}
	}

	return nil
}

func (ps *PackStorage) markerIDs(ext string) (map[string]bool, error) {
	names, err := ps.storage.List(ext)
	if err != nil {
		return nil, err
	}

	ids := make(map[string]bool, len(names))
	for _, name := range names {
		ids[strings.TrimSuffix(name, ext)] = true
	}

	return ids, nil
}

func (ps *PackStorage) readBeginMarker(id string) (*beginMarker, error) {
	file, err := ps.storage.Open(id + SessionBeginExt)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var marker beginMarker
	if err := json.NewDecoder(file).Decode(&marker); err != nil {
		return nil, fmt.Errorf("reading the begin of session %s: %w", id, err)
	}

	return &marker, nil
}

func (ps *PackStorage) readBegin(id string) (*backup.Session, error) {
	marker, err := ps.readBeginMarker(id)
	if err != nil {
		return nil, err
	}

	s := &backup.Session{ID: marker.ID, AgentID: marker.AgentID, Set: marker.Set, Started: marker.Started, LastSeen: marker.Started}
	if len(marker.Restore) > 0 {
		s.Restore = &proto.Ref{Hash: marker.Restore}
	}

	return s, nil
}
