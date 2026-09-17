package pack

import (
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/proto"

	"golang.org/x/crypto/chacha20poly1305"
)

// ErrAtRestKeyMissing is returned when a record is sealed at rest and the
// store was opened without a key.
var ErrAtRestKeyMissing = errors.New("record is sealed at rest and the store has no key")

// ErrAtRestKeyMismatch is returned when a record is sealed under another
// key than the store's.
var ErrAtRestKeyMismatch = errors.New("record is sealed at rest under another key")

// AtRestKey seals every payload on its way into an archive and opens it on
// the way out; headers stay readable, so an index can be rebuilt without
// the key. It is the server's own layer: an agent's sealed blobs are
// sealed once more, and what an agent sent in plaintext is plaintext
// again for every reader with the key.
type AtRestKey struct {
	id   []byte
	aead cipher.AEAD
}

// NewAtRestKey wraps a key file's key; the key id marks the records
// sealed under it.
func NewAtRestKey(key *storekey.Key) *AtRestKey {
	aead, err := chacha20poly1305.NewX(key.Bytes())
	if err != nil {
		panic(err)
	}

	return &AtRestKey{id: key.ID(), aead: aead}
}

// ID marks the records sealed under this key.
func (k *AtRestKey) ID() []byte {
	return k.id
}

func atRestAAD(hdr *proto.ObjectHeader) []byte {
	return append(append([]byte(nil), hdr.Ref.GetHash()...), byte(hdr.Type))
}

// seal returns nonce || ciphertext for a payload, bound to the record's
// ref and type.
func (k *AtRestKey) seal(hdr *proto.ObjectHeader, payload []byte) ([]byte, error) {
	nonce := make([]byte, chacha20poly1305.NonceSizeX, chacha20poly1305.NonceSizeX+len(payload)+chacha20poly1305.Overhead)
	_, err := rand.Read(nonce)
	if err != nil {
		return nil, err
	}

	return k.aead.Seal(nonce, nonce, payload, atRestAAD(hdr)), nil
}

func (k *AtRestKey) open(hdr *proto.ObjectHeader, stored []byte) ([]byte, error) {
	if len(stored) < chacha20poly1305.NonceSizeX+chacha20poly1305.Overhead {
		return nil, fmt.Errorf("record %x is too short to be sealed at rest", hdr.Ref.GetHash())
	}

	nonce, ciphertext := stored[:chacha20poly1305.NonceSizeX], stored[chacha20poly1305.NonceSizeX:]

	payload, err := k.aead.Open(nil, nonce, ciphertext, atRestAAD(hdr))
	if err != nil {
		return nil, fmt.Errorf("opening record %x sealed at rest: %w", hdr.Ref.GetHash(), err)
	}

	return payload, nil
}

// openAtRest returns a stored payload as the agent sent it: opened under
// key when the header says it is sealed, as is otherwise.
func openAtRest(key *AtRestKey, hdr *proto.ObjectHeader, stored []byte) ([]byte, error) {
	switch {
	case len(hdr.AtRestKeyId) == 0:
		return stored, nil
	case key == nil:
		return nil, fmt.Errorf("%w: record %x", ErrAtRestKeyMissing, hdr.Ref.GetHash())
	case string(hdr.AtRestKeyId) != string(key.id):
		return nil, fmt.Errorf("%w: record %x under key %x", ErrAtRestKeyMismatch, hdr.Ref.GetHash(), hdr.AtRestKeyId)
	}

	return key.open(hdr, stored)
}
