package sql

import (
	"context"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/index/sql/ent"
	"github.com/twcclan/goback/index/sql/ent/archive"
	"github.com/twcclan/goback/index/sql/ent/object"
	"github.com/twcclan/goback/index/sql/ent/predicate"
	"github.com/twcclan/goback/index/sql/ent/publicref"
	"github.com/twcclan/goback/index/sql/ent/session"
	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/storage/pack"
)

// objectBatch is how many object rows one insert carries.
const objectBatch = 1000

// LocateObject implements pack.ArchiveIndex: the committed archives plus
// the pending archives of the scope's session.
func (x *Index) LocateObject(ref *proto.Ref, scope pack.Scope, exclude ...string) (pack.IndexLocation, error) {
	ctx := context.Background()

	visible := archive.State(int(pack.ArchiveCommitted))
	if scope.Session != "" {
		visible = archive.Or(visible, archive.SessionID(scope.Session))
	}

	where := []predicate.Object{object.Ref(ref.Hash), object.HasArchiveWith(visible)}
	if len(exclude) > 0 {
		where = append(where, object.ArchiveIDNotIn(exclude...))
	}

	row, err := x.client.Object.Query().Where(where...).Order(ent.Asc(object.FieldID)).First(ctx)
	if ent.IsNotFound(err) {
		return pack.IndexLocation{}, pack.ErrRecordNotFound
	}

	if err != nil {
		return pack.IndexLocation{}, err
	}

	return m.Location(row), nil
}

// LookupArchive implements pack.ArchiveIndex.
func (x *Index) LookupArchive(name string) (pack.ArchiveInfo, bool, error) {
	row, err := x.client.Archive.Get(context.Background(), name)
	if ent.IsNotFound(err) {
		return pack.ArchiveInfo{}, false, nil
	}

	if err != nil {
		return pack.ArchiveInfo{}, false, err
	}

	return m.Archive(row), true, nil
}

// IndexArchive implements pack.ArchiveIndex; a pending archive needs a
// live session.
func (x *Index) IndexArchive(info pack.ArchiveInfo, index pack.IndexFile) error {
	ctx := context.Background()

	// the archive row and its objects land together: a known archive
	// without objects would never be indexed again
	return x.tx(ctx, func(tx *ent.Tx) error {
		if info.Session != "" {
			live, err := tx.Session.Query().Where(session.ID(info.Session)).Exist(ctx)
			if err != nil {
				return err
			}

			if !live {
				return backup.ErrNoSession
			}
		}

		exists, err := tx.Archive.Query().Where(archive.ID(info.Name)).Exist(ctx)
		if err != nil || exists {
			return err
		}

		err = tx.Archive.Create().SetID(info.Name).SetNillableSessionID(nilIfZero(info.Session)).SetState(int(info.State)).Exec(ctx)
		if err != nil {
			return err
		}

		for len(index) > 0 {
			stop := min(len(index), objectBatch)

			builders := make([]*ent.ObjectCreate, stop)
			for i, record := range index[:stop] {
				builders[i] = tx.Object.Create().SetRef(record.Sum[:]).SetArchiveID(info.Name).SetStart(record.Offset).SetLength(record.Length).SetType(record.Type)
			}

			err := tx.Object.CreateBulk(builders...).OnConflict().DoNothing().Exec(ctx)
			if err != nil {
				return err
			}

			index = index[stop:]
		}

		return nil
	})
}

// DeleteArchive implements pack.ArchiveIndex; the objects go with the
// archive.
func (x *Index) DeleteArchive(name string, _ pack.IndexFile) error {
	ctx := context.Background()

	return x.tx(ctx, func(tx *ent.Tx) error {
		if _, err := tx.Object.Delete().Where(object.ArchiveID(name)).Exec(ctx); err != nil {
			return err
		}

		_, err := tx.Archive.Delete().Where(archive.ID(name)).Exec(ctx)

		return err
	})
}

// BeginSession implements pack.SessionIndex.
func (x *Index) BeginSession(s *backup.Session) error {
	return x.client.Session.Create().SetID(s.ID).SetAgentID(s.AgentID).SetBackupSet(s.Set).
		SetStartedAt(s.Started.UTC()).SetLastSeen(s.LastSeen.UTC()).SetRestoreRef(s.Restore.GetHash()).Exec(context.Background())
}

// TouchSession implements pack.SessionIndex.
func (x *Index) TouchSession(id string, at time.Time) error {
	n, err := x.client.Session.Update().Where(session.ID(id)).SetLastSeen(at.UTC()).Save(context.Background())
	if err == nil && n == 0 {
		return backup.ErrNoSession
	}

	return err
}

// GetSession implements pack.SessionIndex.
func (x *Index) GetSession(id string) (*backup.Session, error) {
	row, err := x.client.Session.Get(context.Background(), id)
	if ent.IsNotFound(err) {
		return nil, backup.ErrNoSession
	}

	if err != nil {
		return nil, err
	}

	return m.Session(row), nil
}

// ListSessions implements pack.SessionIndex.
func (x *Index) ListSessions() ([]*backup.Session, error) {
	rows, err := x.client.Session.Query().All(context.Background())
	if err != nil {
		return nil, err
	}

	return mapAll(rows, m.Session), nil
}

// EndSession implements pack.SessionIndex: the session's pending archives
// go with it.
func (x *Index) EndSession(id string) ([]string, error) {
	ctx := context.Background()

	var dropped []string

	err := x.tx(ctx, func(tx *ent.Tx) error {
		pending, err := tx.Archive.Query().Where(archive.SessionID(id), archive.State(int(pack.ArchivePending))).IDs(ctx)
		if err != nil {
			return err
		}

		dropped = pending

		if _, err := tx.Object.Delete().Where(object.ArchiveIDIn(pending...)).Exec(ctx); err != nil {
			return err
		}

		if _, err := tx.Archive.Delete().Where(archive.IDIn(pending...)).Exec(ctx); err != nil {
			return err
		}

		_, err = tx.Session.Delete().Where(session.ID(id)).Exec(ctx)

		return err
	})

	return dropped, err
}

// CommitSession implements pack.SessionIndex: the session must still be
// live, then its pending archives become committed.
func (x *Index) CommitSession(id string) error {
	ctx := context.Background()

	return x.tx(ctx, func(tx *ent.Tx) error {
		live, err := tx.Session.Query().Where(session.ID(id)).Exist(ctx)
		if err != nil {
			return err
		}

		if !live {
			return backup.ErrNoSession
		}

		return tx.Archive.Update().Where(archive.SessionID(id)).SetState(int(pack.ArchiveCommitted)).ClearSessionID().Exec(ctx)
	})
}

// RecordPublicRefs implements pack.PublicRefIndex.
func (x *Index) RecordPublicRefs(refs [][]byte) error {
	if len(refs) == 0 {
		return nil
	}

	ctx := context.Background()

	return x.tx(ctx, func(tx *ent.Tx) error {
		builders := make([]*ent.PublicRefCreate, len(refs))
		for i, ref := range refs {
			builders[i] = tx.PublicRef.Create().SetRef(ref)
		}

		return tx.PublicRef.CreateBulk(builders...).OnConflict().DoNothing().Exec(ctx)
	})
}

// HasPublicRef implements pack.PublicRefIndex.
func (x *Index) HasPublicRef(ref []byte) (bool, error) {
	return x.client.PublicRef.Query().Where(publicref.Ref(ref)).Exist(context.Background())
}

// ForgetRefs implements pack.PublicRefIndex.
func (x *Index) ForgetRefs(refs [][]byte) error {
	if len(refs) == 0 {
		return nil
	}

	_, err := x.client.PublicRef.Delete().Where(publicref.RefIn(refs...)).Exec(context.Background())

	return err
}

// CountObjects implements pack.ArchiveIndex: the object rows and the
// distinct refs among them.
func (x *Index) CountObjects() (uint64, uint64, error) {
	ctx := context.Background()

	total, err := x.client.Object.Query().Count(ctx)
	if err != nil {
		return 0, 0, err
	}

	unique, err := x.client.Object.Query().Unique(true).Select(object.FieldRef).Count(ctx)
	if err != nil {
		return 0, 0, err
	}

	return uint64(total), uint64(unique), nil
}
