package sql

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/index/sql/ent"
	"github.com/twcclan/goback/index/sql/ent/commitrow"
	"github.com/twcclan/goback/index/sql/ent/set"
	"github.com/twcclan/goback/proto"

	pb "google.golang.org/protobuf/proto"
)

// storePresence records a commit's filter and drops the filters of the
// set's other commits, so only the head carries one; a filter of a commit
// older than the one that carries a filter is dropped instead.
func storePresence(ctx context.Context, x *Index, setID int64, ref *proto.Ref, filter *proto.PresenceFilter) error {
	data, err := pb.Marshal(filter)
	if err != nil {
		return err
	}

	return x.tx(ctx, func(tx *ent.Tx) error {
		if _, err := x.lockSet(ctx, tx, setID); err != nil {
			return err
		}

		row, err := tx.CommitRow.Query().Where(commitrow.Ref(ref.Hash), commitrow.SetID(setID), commitrow.TombstonedAtIsNil()).Only(ctx)
		if ent.IsNotFound(err) {
			return nil
		}

		if err != nil {
			return err
		}

		newer, err := tx.CommitRow.Query().Where(commitrow.SetID(setID), commitrow.PresenceNotNil(), commitrow.ReceivedAtGT(row.ReceivedAt)).Exist(ctx)
		if err != nil || newer {
			return err
		}

		err = tx.CommitRow.Update().Where(commitrow.Ref(ref.Hash)).SetPresence(data).Exec(ctx)
		if err != nil {
			return err
		}

		return tx.CommitRow.Update().Where(commitrow.SetID(setID), commitrow.RefNEQ(ref.Hash), commitrow.PresenceNotNil()).ClearPresence().Exec(ctx)
	})
}

// loadPresence returns the stored filters of every set in the scope.
func loadPresence(ctx context.Context, c *ent.Client, scope backup.PresenceScope, name string) ([]*proto.PresenceFilter, error) {
	query := c.CommitRow.Query().Where(commitrow.PresenceNotNil())

	switch scope {
	case backup.PresenceSet:
		query.Where(commitrow.HasSetWith(set.Name(name)))
	case backup.PresenceStore:
	default:
		return nil, nil
	}

	rows, err := query.Select(commitrow.FieldPresence).All(ctx)
	if err != nil {
		return nil, err
	}

	var filters []*proto.PresenceFilter
	for _, row := range rows {
		filter := &proto.PresenceFilter{}
		if err := pb.Unmarshal(row.Presence, filter); err != nil {
			return nil, fmt.Errorf("decoding presence filter: %w", err)
		}

		filters = append(filters, filter)
	}

	return filters, nil
}

// PresenceBuilder builds commit filters in the background, one build per
// set at a time with the newest commit winning, and stores them; the same
// walk records the commit's logical size.
type PresenceBuilder struct {
	index *Index

	mtx     sync.Mutex
	running map[int64]bool
	next    map[int64]presenceJob
	wg      sync.WaitGroup
}

type presenceJob struct {
	setID  int64
	set    string
	commit *proto.Ref
	tree   *proto.Ref
}

// Schedule builds the filter of a set's newest commit in the background.
func (b *PresenceBuilder) Schedule(setID int64, name string, commit, tree *proto.Ref) {
	job := presenceJob{setID: setID, set: name, commit: commit, tree: tree}

	b.mtx.Lock()
	defer b.mtx.Unlock()

	if b.running[setID] {
		b.next[setID] = job
		return
	}

	b.running[setID] = true
	b.wg.Add(1)

	go b.run(job)
}

// Wait blocks until every scheduled build has finished.
func (b *PresenceBuilder) Wait() {
	b.wg.Wait()
}

func (b *PresenceBuilder) run(job presenceJob) {
	defer b.wg.Done()

	for {
		b.build(job)

		b.mtx.Lock()
		next, ok := b.next[job.setID]
		delete(b.next, job.setID)
		if !ok {
			delete(b.running, job.setID)
		}
		b.mtx.Unlock()

		if !ok {
			return
		}

		job = next
	}
}

func (b *PresenceBuilder) build(job presenceJob) {
	ctx := context.Background()
	start := time.Now()

	filter, size, err := backup.CollectPresence(ctx, b.index.ObjectStore, job.tree)
	if err != nil {
		log.Printf("Cannot build presence filter for commit %x of set %s: %v", job.commit.Hash, job.set, err)
		return
	}

	err = b.index.client.CommitRow.Update().Where(commitrow.Ref(job.commit.Hash)).SetLogicalSize(size).Exec(ctx)
	if err != nil {
		log.Printf("Cannot record the size of commit %x of set %s: %v", job.commit.Hash, job.set, err)
		return
	}

	filter.Commit = job.commit
	filter.Set = job.set

	err = storePresence(ctx, b.index, job.setID, job.commit, filter.Proto())
	if err != nil {
		log.Printf("Cannot store presence filter for commit %x of set %s: %v", job.commit.Hash, job.set, err)
		return
	}

	log.Printf("Built presence filter for set %s: %d refs, %d bytes in %v", job.set, filter.Entries(), filter.Size(), time.Since(start).Round(time.Millisecond))
}
