package pack

import (
	"context"

	"github.com/twcclan/goback/proto"

	"github.com/pkg/errors"
)

// RepairReport summarises one repair pass.
type RepairReport struct {
	// Scrub is the pass that found the damage.
	Scrub *ScrubReport
	// Archives is how many archives were rewritten.
	Archives uint64
	// Recovered names the objects the store still holds after the pass,
	// because another archive had an intact copy.
	Recovered []*proto.Ref
	// Lost names the objects no archive holds any more. A restore of one
	// leaves a hole until a backup of a source that still has it puts it
	// back.
	Lost []*proto.Ref
}

// Repair scrubs the store and rewrites every archive holding damage,
// leaving the corrupt records behind. Objects an intact copy exists for
// survive the pass; the rest are gone from the store, so a later backup
// stores them again instead of finding them present.
func (ps *PackStorage) Repair(ctx context.Context) (*RepairReport, error) {
	report, err := ps.Scrub(ctx)
	if err != nil {
		return nil, err
	}

	return ps.repair(ctx, report)
}

func (ps *PackStorage) repair(ctx context.Context, scrub *ScrubReport) (*RepairReport, error) {
	report := &RepairReport{Scrub: scrub}

	damaged := make(map[string]map[string]bool)
	var dropped []*proto.Ref

	for _, failure := range scrub.Corrupt {
		if damaged[failure.Archive] == nil {
			damaged[failure.Archive] = make(map[string]bool)
		}

		// a rewrite re-links every record, so a broken chain is repaired
		// by the rewrite itself and costs no record
		if errors.Is(failure.Err, ErrBrokenChain) {
			continue
		}

		if !damaged[failure.Archive][string(failure.Ref.GetHash())] {
			dropped = append(dropped, failure.Ref)
		}

		damaged[failure.Archive][string(failure.Ref.GetHash())] = true
	}

	if len(damaged) == 0 {
		return report, nil
	}

	group := &compactionGroup{
		keep: func(candidate *archive, hdr *proto.ObjectHeader) bool {
			return !damaged[candidate.name][string(hdr.Ref.GetHash())]
		},
	}

	ps.mtx.RLock()
	for _, candidate := range ps.archives {
		if _, ok := damaged[candidate.name]; ok {
			group.candidates = append(group.candidates, candidate)
			group.total += candidate.size
		}
	}
	ps.mtx.RUnlock()

	ps.compactorMtx.Lock()
	err := ps.compactGroup(ctx, group)
	ps.compactorMtx.Unlock()

	if err != nil {
		return nil, errors.Wrap(err, "rewriting the damaged archives")
	}

	report.Archives = uint64(len(group.candidates))

	for _, ref := range dropped {
		has, err := ps.Has(ctx, ref)
		if err != nil {
			return nil, err
		}

		if has {
			report.Recovered = append(report.Recovered, ref)
		} else {
			report.Lost = append(report.Lost, ref)
		}
	}

	return report, nil
}
