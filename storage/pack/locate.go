package pack

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
)

// ErrNoSignedURL is a RangeSigner's answer when it cannot address the
// bytes it holds.
var ErrNoSignedURL = errors.New("this storage hands out no URLs")

// locationTTL is how long a location is good for. It is short on purpose:
// one is minted to be followed at once, not carried around.
const locationTTL = time.Minute

// A RangeSigner is an ArchiveStorage whose files can be fetched directly,
// one byte range at a time.
type RangeSigner interface {
	// SignRange returns a GET whose body is bytes [offset, offset+length)
	// of name, good for ttl, carrying whatever headers the request must
	// repeat. It returns ErrNoSignedURL when it cannot sign.
	SignRange(ctx context.Context, name string, offset, length int64, ttl time.Duration) (*proto.Location, error)
}

var _ backup.Locator = (*PackStorage)(nil)

// Read implements backup.Locator. An object sitting in an archive that
// can be addressed is answered as a location, carrying the archive's key
// when it is sealed at rest; everything else is read and answered as the
// object.
func (ps *PackStorage) Read(ctx context.Context, ref *proto.Ref) (*proto.Object, *proto.Location, error) {
	location, err := ps.locate(ctx, ref)
	if err != nil || location != nil {
		return nil, location, err
	}

	object, err := ps.Get(ctx, ref)

	return object, nil, err
}

func (ps *PackStorage) locate(ctx context.Context, ref *proto.Ref) (*proto.Location, error) {
	signer, ok := ps.storage.(RangeSigner)
	if !ok {
		return nil, nil
	}

	a, rec, err := ps.committedCopy(ScopeOf(ctx), ref, true)
	if err != nil || a == nil {
		return nil, err
	}

	// a location says nothing about what it addresses, so the caller must
	// not be sent to bytes an ordinary read would have refused
	switch proto.ObjectType(rec.Type) {
	case proto.ObjectType_COMMIT, proto.ObjectType_TREE, proto.ObjectType_FILE:
	default:
		return nil, nil
	}

	location, err := signer.SignRange(ctx, a.archiveName(), int64(rec.Offset), int64(rec.Length), locationTTL)
	if errors.Is(err, ErrNoSignedURL) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	if a.atRest != nil {
		location.AtRestKey = a.atRest.shared
	}

	return location, nil
}

const (
	// runGap is how far apart two records may sit and still share a run;
	// runSpan bounds the bytes one run covers.
	runGap  = 64 << 10
	runSpan = 16 << 20

	// runTTL outlasts locationTTL because a reader follows a file's runs
	// a few at a time, so the last of them waits behind the others.
	runTTL = 15 * time.Minute
)

var _ backup.RecordLocator = (*PackStorage)(nil)

type locatedRecord struct {
	index int
	rec   *IndexRecord
}

// LocateRecords implements backup.RecordLocator for blobs in archives the
// storage can sign ranges of.
func (ps *PackStorage) LocateRecords(ctx context.Context, refs []*proto.Ref) ([]*proto.LocatedRun, error) {
	signer, ok := ps.storage.(RangeSigner)
	if !ok {
		return nil, nil
	}

	byArchive := make(map[*archive][]locatedRecord)

	for i, ref := range refs {
		a, rec, err := ps.committedCopy(ScopeOf(ctx), ref, true)
		if err != nil {
			return nil, err
		}

		if a == nil || proto.ObjectType(rec.Type) != proto.ObjectType_BLOB {
			continue
		}

		byArchive[a] = append(byArchive[a], locatedRecord{index: i, rec: rec})
	}

	var runs []*proto.LocatedRun

	for a, records := range byArchive {
		sort.Slice(records, func(i, j int) bool { return records[i].rec.Offset < records[j].rec.Offset })

		for start := 0; start < len(records); {
			end, span := runOf(records, start)

			run, err := ps.signRun(ctx, signer, a, records[start:end], span)
			if errors.Is(err, ErrNoSignedURL) {
				return nil, nil
			}

			if err != nil {
				return nil, err
			}

			runs = append(runs, run)
			start = end
		}
	}

	return runs, nil
}

// runOf extends the run starting at start while the records stay close
// together, and returns where it ends and how many bytes it covers. The
// same record twice overlaps itself, which a run takes in its stride.
func runOf(records []locatedRecord, start int) (int, int64) {
	from := int64(records[start].rec.Offset)
	reach := from + int64(records[start].rec.Length)

	end := start + 1
	for ; end < len(records); end++ {
		offset := int64(records[end].rec.Offset)
		grown := max(reach, offset+int64(records[end].rec.Length))

		if offset-reach > runGap || grown-from > runSpan {
			break
		}

		reach = grown
	}

	return end, reach - from
}

func (ps *PackStorage) signRun(ctx context.Context, signer RangeSigner, a *archive, records []locatedRecord, span int64) (*proto.LocatedRun, error) {
	from := int64(records[0].rec.Offset)

	location, err := signer.SignRange(ctx, a.archiveName(), from, span, runTTL)
	if err != nil {
		return nil, err
	}

	if a.atRest != nil {
		location.AtRestKey = a.atRest.shared
	}

	run := &proto.LocatedRun{Location: location, Records: make([]*proto.LocatedRecord, len(records))}
	for i, r := range records {
		run.Records[i] = &proto.LocatedRecord{Index: uint32(r.index), Offset: int64(r.rec.Offset) - from, Length: int64(r.rec.Length)}
	}

	return run, nil
}

// DecodeRecord turns the body a Location yields back into its object,
// opening it with the location's at-rest key when it is sealed.
func DecodeRecord(record []byte, atRestKey []byte) (*proto.Object, error) {
	size, consumed := proto.DecodeVarint(record)
	if consumed <= 0 || uint64(len(record)) < uint64(consumed)+size {
		return nil, errors.New("record is truncated")
	}

	header, err := proto.NewObjectHeaderFromBytes(record[consumed : uint64(consumed)+size])
	if err != nil {
		return nil, err
	}

	var key *archiveKey
	if len(atRestKey) != 0 {
		key, err = parseArchiveKey(atRestKey)
		if err != nil {
			return nil, err
		}
	}

	stored, err := openAtRest(key, header, record[uint64(consumed)+size:])
	if err != nil {
		return nil, err
	}

	return proto.ObjectFromStored(header, stored)
}
