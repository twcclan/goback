package sql

import (
	"context"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/index/sql/ent"
	"github.com/twcclan/goback/index/sql/ent/archive"
	"github.com/twcclan/goback/index/sql/ent/object"
	"github.com/twcclan/goback/index/sql/ent/predicate"
	"github.com/twcclan/goback/index/sql/ent/session"
	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/storage/pack"
)

// objectBatch is how many object rows one insert carries.
const objectBatch = 1000

var _ pack.ClaimIndex = (*Index)(nil)

// LocateObject implements pack.ArchiveIndex: the committed archives plus
// the pending archives of the scope's session.
func (x *Index) LocateObject(ref *proto.Ref, scope pack.Scope, exclude ...string) (pack.IndexLocation, error) {
	ctx := context.Background()

	visible := archive.State(int(pack.ArchiveCommitted))
	if scope.Session != "" {
		visible = archive.Or(visible, archive.And(archive.SessionID(scope.Session), archive.State(int(pack.ArchivePending))))
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

// LocateCopies implements pack.ArchiveIndex.
func (x *Index) LocateCopies(refs []*proto.Ref) (map[string][]pack.IndexLocation, error) {
	ctx := context.Background()
	copies := make(map[string][]pack.IndexLocation)

	for start := 0; start < len(refs); start += objectBatch {
		chunk := refs[start:min(start+objectBatch, len(refs))]

		hashes := make([][]byte, len(chunk))
		for i, ref := range chunk {
			hashes[i] = ref.Hash
		}

		rows, err := x.client.Object.Query().
			Where(object.RefIn(hashes...), object.HasArchiveWith(archive.State(int(pack.ArchiveCommitted)))).
			All(ctx)
		if err != nil {
			return nil, err
		}

		for _, row := range rows {
			copies[string(row.Ref)] = append(copies[string(row.Ref)], m.Location(row))
		}
	}

	return copies, nil
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

		return insertObjects(ctx, tx, info.Name, index)
	})
}

// insertObjects adds the object rows of an archive, in batches.
func insertObjects(ctx context.Context, tx *ent.Tx, name string, records []pack.IndexRecord) error {
	for len(records) > 0 {
		stop := min(len(records), objectBatch)

		builders := make([]*ent.ObjectCreate, stop)
		for i, record := range records[:stop] {
			builders[i] = tx.Object.Create().SetRef(record.Sum[:]).SetArchiveID(name).SetStart(record.Offset).SetLength(record.Length).SetType(record.Type)
		}

		err := tx.Object.CreateBulk(builders...).OnConflict().DoNothing().Exec(ctx)
		if err != nil {
			return err
		}

		records = records[stop:]
	}

	return nil
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

// EndSession implements pack.SessionIndex: the session's uncommitted
// archives go with it.
func (x *Index) EndSession(id string) ([]string, error) {
	ctx := context.Background()

	var dropped []string

	err := x.tx(ctx, func(tx *ent.Tx) error {
		pending, err := tx.Archive.Query().Where(archive.SessionID(id), archive.StateNEQ(int(pack.ArchiveCommitted))).IDs(ctx)
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

// PendingArchives implements pack.SessionIndex.
func (x *Index) PendingArchives(id string) ([]string, error) {
	return x.client.Archive.Query().Where(archive.SessionID(id), archive.State(int(pack.ArchivePending))).IDs(context.Background())
}

// CommitSession implements pack.SessionIndex: the session must still be
// live, then its pending archives become committed and its lost ones go.
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

		lost, err := tx.Archive.Query().Where(archive.SessionID(id), archive.State(int(pack.ArchiveLost))).IDs(ctx)
		if err != nil {
			return err
		}

		if _, err := tx.Object.Delete().Where(object.ArchiveIDIn(lost...)).Exec(ctx); err != nil {
			return err
		}

		if _, err := tx.Archive.Delete().Where(archive.IDIn(lost...)).Exec(ctx); err != nil {
			return err
		}

		return tx.Archive.Update().Where(archive.SessionID(id), archive.State(int(pack.ArchivePending))).
			SetState(int(pack.ArchiveCommitted)).ClearSessionID().Exec(ctx)
	})
}

// CountObjects implements pack.ArchiveIndex: the object rows and the
// distinct refs among them.
func (x *Index) CountObjects() (uint64, uint64, error) {
	ctx := context.Background()

	finalized := object.HasArchiveWith(archive.StateIn(int(pack.ArchiveCommitted), int(pack.ArchivePending)))

	total, err := x.client.Object.Query().Where(finalized).Count(ctx)
	if err != nil {
		return 0, 0, err
	}

	unique, err := x.client.Object.Query().Where(finalized).Unique(true).Select(object.FieldRef).Count(ctx)
	if err != nil {
		return 0, 0, err
	}

	return uint64(total), uint64(unique), nil
}

// clock is the time claims are aged by: the database's own clock on
// Postgres, which every process shares, and Now on SQLite, which only one
// machine opens.
func (x *Index) clock(ctx context.Context) (time.Time, error) {
	if !x.locking {
		return x.now().UTC(), nil
	}

	var now time.Time
	if err := x.db.QueryRowContext(ctx, "SELECT now()").Scan(&now); err != nil {
		return time.Time{}, err
	}

	return now.UTC(), nil
}

// OpenArchive implements pack.ClaimIndex.
func (x *Index) OpenArchive(name, id string) error {
	ctx := context.Background()

	now, err := x.clock(ctx)
	if err != nil {
		return err
	}

	return x.tx(ctx, func(tx *ent.Tx) error {
		live, err := tx.Session.Query().Where(session.ID(id)).Exist(ctx)
		if err != nil {
			return err
		}

		if !live {
			return backup.ErrNoSession
		}

		return tx.Archive.Create().SetID(name).SetSessionID(id).SetState(int(pack.ArchiveOpen)).SetOpenedAt(now).Exec(ctx)
	})
}

// AddObjects implements pack.ClaimIndex. The archive's row is locked for
// the insert, so a finalize or an abandon cannot come between the check
// and the rows.
func (x *Index) AddObjects(name string, records []pack.IndexRecord) error {
	ctx := context.Background()

	return x.tx(ctx, func(tx *ent.Tx) error {
		query := tx.Archive.Query().Where(archive.ID(name), archive.State(int(pack.ArchiveOpen)))
		if x.locking {
			query = query.ForUpdate()
		}

		open, err := query.Exist(ctx)
		if err != nil {
			return err
		}

		if !open {
			return pack.ErrClaimLapsed
		}

		return insertObjects(ctx, tx, name, records)
	})
}

// FinalizeArchive implements pack.ClaimIndex.
func (x *Index) FinalizeArchive(name string, within time.Duration) error {
	ctx := context.Background()

	now, err := x.clock(ctx)
	if err != nil {
		return err
	}

	n, err := x.client.Archive.Update().
		Where(archive.ID(name), archive.State(int(pack.ArchiveOpen)), archive.OpenedAtGT(now.Add(-within))).
		SetState(int(pack.ArchivePending)).ClearOpenedAt().Save(ctx)
	if err != nil {
		return err
	}

	if n == 0 {
		return pack.ErrClaimLapsed
	}

	return nil
}

// Holds implements pack.ClaimIndex.
func (x *Index) Holds(ref *proto.Ref, id string) (bool, error) {
	return x.client.Object.Query().Where(
		object.Ref(ref.Hash),
		object.HasArchiveWith(archive.SessionID(id), archive.StateIn(int(pack.ArchivePending), int(pack.ArchiveOpen))),
	).Exist(context.Background())
}

// Claims implements pack.ClaimIndex.
func (x *Index) Claims(id string) ([]pack.Claim, error) {
	ctx := context.Background()

	rows, err := x.client.Archive.Query().Where(archive.SessionID(id), archive.State(int(pack.ArchiveOpen))).All(ctx)
	if err != nil {
		return nil, err
	}

	now, err := x.clock(ctx)
	if err != nil {
		return nil, err
	}

	claims := make([]pack.Claim, len(rows))
	for i, row := range rows {
		claims[i] = pack.Claim{Archive: row.ID}
		if row.OpenedAt != nil {
			claims[i].Age = now.Sub(*row.OpenedAt)
		}
	}

	return claims, nil
}

// Abandon implements pack.ClaimIndex.
func (x *Index) Abandon(name string, within time.Duration) (bool, error) {
	ctx := context.Background()

	now, err := x.clock(ctx)
	if err != nil {
		return false, err
	}

	n, err := x.client.Archive.Update().
		Where(archive.ID(name), archive.State(int(pack.ArchiveOpen)), archive.OpenedAtLTE(now.Add(-within))).
		SetState(int(pack.ArchiveLost)).Save(ctx)

	return n > 0, err
}

// Lost implements pack.ClaimIndex.
func (x *Index) Lost(id string) ([]*proto.Ref, error) {
	ctx := context.Background()

	lostRows, err := x.client.Object.Query().
		Where(
			object.HasArchiveWith(archive.SessionID(id), archive.State(int(pack.ArchiveLost))),
			object.TypeNEQ(uint32(proto.ObjectType_COMMIT)),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}

	kept := archive.Or(
		archive.State(int(pack.ArchiveCommitted)),
		archive.And(archive.SessionID(id), archive.State(int(pack.ArchivePending))),
	)

	seen := make(map[string]bool)

	var lost []*proto.Ref
	for _, row := range lostRows {
		if seen[string(row.Ref)] {
			continue
		}

		seen[string(row.Ref)] = true

		held, err := x.client.Object.Query().Where(object.Ref(row.Ref), object.HasArchiveWith(kept)).Exist(ctx)
		if err != nil {
			return nil, err
		}

		if !held {
			lost = append(lost, &proto.Ref{Hash: row.Ref})
		}
	}

	return lost, nil
}
