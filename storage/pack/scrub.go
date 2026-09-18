package pack

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/twcclan/goback/proto"

	"github.com/pkg/errors"
)

// ScrubReport summarises one pass over every stored object.
type ScrubReport struct {
	Archives uint64
	Objects  uint64
	Bytes    uint64
	// Corrupt lists the objects whose stored bytes do not hash to their
	// header or whose header names the wrong predecessor.
	Corrupt []ScrubFailure
	// Sealed counts the objects per hex at-rest key id, the ones stored
	// in the clear under the empty string. A rotation is over once no
	// retired key id is left here.
	Sealed map[string]uint64
}

// ScrubFailure names one object that failed verification.
type ScrubFailure struct {
	Archive string
	Ref     *proto.Ref
	Err     error
}

// ErrBrokenChain is the failure of an object whose header does not name the
// object written before it in the archive.
var ErrBrokenChain = errors.New("object header names the wrong predecessor")

// Scrub rehashes every object in every archive and reports the ones that no
// longer match their ref or break their archive's predecessor chain. It
// never modifies the store.
func (ps *PackStorage) Scrub(ctx context.Context) (*ScrubReport, error) {
	ps.mtx.RLock()
	archives := append([]*archive(nil), ps.archives...)
	ps.mtx.RUnlock()

	report := &ScrubReport{Sealed: make(map[string]uint64)}

	for _, a := range archives {
		if err := ctx.Err(); err != nil {
			return report, err
		}

		ps.logger.Info("scrubbing archive", "archive", a.name)
		report.Archives++

		var prev *proto.Ref
		err := a.foreach(loadAll, func(hdr *proto.ObjectHeader, bytes []byte, offset, length uint32) error {
			report.Objects++
			report.Bytes += uint64(length)
			report.Sealed[hex.EncodeToString(hdr.AtRestKeyId)]++

			err := proto.VerifyStored(hdr, bytes)
			if err != nil {
				ps.logger.Warn("corrupt object", "ref", fmt.Sprintf("%x", hdr.Ref.GetHash()), "archive", a.name, "offset", offset, "err", err)
				report.Corrupt = append(report.Corrupt, ScrubFailure{Archive: a.name, Ref: hdr.Ref, Err: err})
			}

			if !hdr.Predecessor.Equal(prev) {
				err := errors.Wrapf(ErrBrokenChain, "predecessor %x, previous object %x", hdr.Predecessor.GetHash(), prev.GetHash())
				ps.logger.Warn("broken chain", "ref", fmt.Sprintf("%x", hdr.Ref.GetHash()), "archive", a.name, "offset", offset, "err", err)
				report.Corrupt = append(report.Corrupt, ScrubFailure{Archive: a.name, Ref: hdr.Ref, Err: err})
			}

			prev = hdr.Ref

			return nil
		})
		if err != nil {
			return report, errors.Wrapf(err, "scrubbing archive %s", a.name)
		}
	}

	return report, nil
}
