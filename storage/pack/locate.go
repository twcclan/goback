package pack

import (
	"context"
	"errors"
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
// can be addressed is answered as a location; everything else, including
// anything sealed at rest, is read and answered as the object.
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
	if !ok || ps.atRest != nil {
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

	return location, err
}

// DecodeRecord turns the body a Location yields back into its object. A
// record sealed at rest cannot be read this way, which is why one is
// never handed out as a location.
func DecodeRecord(record []byte) (*proto.Object, error) {
	size, consumed := proto.DecodeVarint(record)
	if consumed <= 0 || uint64(len(record)) < uint64(consumed)+size {
		return nil, errors.New("record is truncated")
	}

	header, err := proto.NewObjectHeaderFromBytes(record[consumed : uint64(consumed)+size])
	if err != nil {
		return nil, err
	}

	if len(header.AtRestKeyId) != 0 {
		return nil, ErrAtRestKeyMissing
	}

	return proto.ObjectFromStored(header, record[uint64(consumed)+size:])
}
