package pack

import (
	"context"
	"log"

	"github.com/twcclan/goback/proto"

	"github.com/pkg/errors"
)

// ScrubReport summarises one pass over every stored object.
type ScrubReport struct {
	Archives uint64
	Objects  uint64
	Bytes    uint64
	// Corrupt lists the refs whose stored bytes do not hash to their header.
	Corrupt []ScrubFailure
}

// ScrubFailure names one object that failed verification.
type ScrubFailure struct {
	Archive string
	Ref     *proto.Ref
	Err     error
}

// Scrub rehashes every object in every archive and reports the ones that no
// longer match their ref. It never modifies the store.
func (ps *PackStorage) Scrub(ctx context.Context) (*ScrubReport, error) {
	ps.mtx.RLock()
	archives := append([]*archive(nil), ps.archives...)
	ps.mtx.RUnlock()

	report := &ScrubReport{}

	for _, a := range archives {
		if err := ctx.Err(); err != nil {
			return report, err
		}

		log.Printf("Scrubbing archive %s", a.name)
		report.Archives++

		err := a.foreach(loadAll, func(hdr *proto.ObjectHeader, bytes []byte, offset, length uint32) error {
			report.Objects++
			report.Bytes += uint64(length)

			err := verifyStored(hdr, bytes)
			if err != nil {
				log.Printf("Corrupt object %x in archive %s at offset %d: %v", hdr.Ref.GetHash(), a.name, offset, err)
				report.Corrupt = append(report.Corrupt, ScrubFailure{Archive: a.name, Ref: hdr.Ref, Err: err})
			}

			return nil
		})
		if err != nil {
			return report, errors.Wrapf(err, "scrubbing archive %s", a.name)
		}
	}

	return report, nil
}
