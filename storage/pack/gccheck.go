package pack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"

	"github.com/twcclan/goback/proto"
)

// HaltName is the file a collection leaves when its pre-delete check finds
// a copy it was about to drop reachable. While it exists no collection
// drops anything; an operator deletes it once the cause is understood.
const HaltName = "collection.halt"

// ErrHalted is what a collection reports when its check halts the store's
// collections.
var ErrHalted = errors.New("collection halted: the pre-delete check found reachable copies the sweep would drop")

type halt struct {
	Generation uint64    `json:"generation"`
	At         time.Time `json:"at"`
	Reachable  []string  `json:"reachable"`
}

// halted returns why the store's collections are halted, "" when they are
// not.
func (ps *PackStorage) halted() (string, error) {
	file, err := ps.storage.Open(HaltName)
	if notExist(err) {
		return "", nil
	}

	if err != nil {
		return "", err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}

	var h halt
	if err := json.Unmarshal(data, &h); err != nil {
		return "halted", nil
	}

	return fmt.Sprintf("halted by generation %d: %d reachable copies", h.Generation, len(h.Reachable)), nil
}

// confirmDrops marks again from the roots there are now and halts the
// store's collections if anything the sweep would drop is reachable.
func (r *gcRun) confirmDrops(ctx context.Context) error {
	opts := r.opts
	opts.Owner = nil
	opts.TempDir = filepath.Join(r.opts.TempDir, "goback-gc-check")

	check := &gcRun{ps: r.ps, opts: opts, gen: r.gen, prev: r.prev, snapshot: r.snapshot,
		archives: make(map[string]*gcArchive), targets: make(map[refKey]bool),
		newestTomb: make(map[refKey]Version), condemning: make(map[refKey]Version),
		condemned: make(map[refKey]Version), condemnedAt: make(map[int64]bool), tombTimes: make(map[int64]bool),
		untombed: make(map[refKey]bool), oldestCopy: make(map[refKey]Version), spent: make(map[recordAt]bool),
		setBytes: make(map[int64]uint64), setDeduplicated: make(map[int64]uint64)}

	if err := check.takeSnapshot(); err != nil {
		return err
	}

	defer func() {
		for _, ga := range check.order {
			ga.lookup.release()
		}
	}()

	if err := check.collectRoots(ctx); err != nil {
		return err
	}

	live, err := check.mark(ctx)
	if err != nil {
		return err
	}
	defer live.close()
	defer os.RemoveAll(check.runDir)

	dropped := make(map[refKey][]string)
	kept := make(map[refKey]bool)

	err = check.scan(live, func(ga *gcArchive, pos int, rec *IndexRecord, _ []Attribution, _ uint64) {
		if proto.ObjectType(rec.Type) == proto.ObjectType_TOMBSTONE {
			return
		}

		key := keyOf(rec.Sum[:])

		planned := r.archives[ga.a.name]
		if planned == nil || !r.droppable(planned, planned.next, pos, rec) {
			kept[key] = true
			return
		}

		dropped[key] = append(dropped[key], ga.a.name)
	}, nil)
	if err != nil {
		return err
	}

	var reachable []string

	for key, archives := range dropped {
		if !kept[key] {
			reachable = append(reachable, fmt.Sprintf("%x in %v", key, archives))
		}
	}

	if len(reachable) == 0 {
		return nil
	}

	r.ps.logger.Error("the pre-delete check found reachable copies the sweep would drop; halting collections",
		"generation", r.gen, "count", len(reachable))

	data, err := json.Marshal(halt{Generation: r.gen, At: time.Now().UTC(), Reachable: reachable})
	if err != nil {
		return err
	}

	if err := r.ps.storage.CreateNew(HaltName, data); err != nil && !errors.Is(err, ErrFileExists) {
		return errors.Wrap(err, "halting collections")
	}

	return ErrHalted
}
