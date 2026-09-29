package pack

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"

	"github.com/twcclan/goback/proto"

	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/insecurecleartextkeyset"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"github.com/tink-crypto/tink-go/v2/tink"
)

// ErrAtRestKeyMissing is returned when a record is sealed at rest and the
// store was opened without a key.
var ErrAtRestKeyMissing = errors.New("record is sealed at rest and the store has no key")

// ErrAtRestKeyMismatch is returned when a record is sealed under a key
// the store's keyset does not hold.
var ErrAtRestKeyMismatch = errors.New("record is sealed at rest under another key")

// AtRestKey seals every payload on its way into an archive and opens it on
// the way out; headers stay readable, so an index can be rebuilt without
// the key. It is the server's own layer: an agent's sealed blobs are
// sealed once more, and what an agent sent in plaintext is plaintext
// again for every reader with the key.
//
// It is a Tink AEAD keyset. Its primary key seals and every key in it
// opens, so rotating is Rotate, and a key can leave the keyset once a
// rewrite has re-sealed the last record naming it (Scrub counts them).
type AtRestKey struct {
	handle *keyset.Handle
	aead   tink.AEAD
	id     []byte
	holds  map[string]bool
}

// NewAtRestKey wraps a keyset of AEAD keys.
func NewAtRestKey(h *keyset.Handle) (*AtRestKey, error) {
	a, err := aead.New(h)
	if err != nil {
		return nil, fmt.Errorf("at-rest key: %w", err)
	}

	info := h.KeysetInfo()
	holds := make(map[string]bool, len(info.GetKeyInfo()))
	for _, key := range info.GetKeyInfo() {
		holds[string(keyID(key.GetKeyId()))] = true
	}

	return &AtRestKey{handle: h, aead: a, id: keyID(info.GetPrimaryKeyId()), holds: holds}, nil
}

func keyID(id uint32) []byte {
	return binary.BigEndian.AppendUint32(nil, id)
}

// GenerateAtRestKey makes a keyset of one fresh key.
func GenerateAtRestKey() (*AtRestKey, error) {
	h, err := keyset.NewHandle(aead.XChaCha20Poly1305KeyTemplate())
	if err != nil {
		return nil, err
	}

	return NewAtRestKey(h)
}

// LoadAtRestKey reads a keyset written by Save.
func LoadAtRestKey(path string) (*AtRestKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	h, err := insecurecleartextkeyset.Read(keyset.NewJSONReader(bytes.NewReader(data)))
	if err != nil {
		return nil, fmt.Errorf("at-rest key %s: %w", path, err)
	}

	return NewAtRestKey(h)
}

// Save writes the keyset, readable by its owner only.
func (k *AtRestKey) Save(path string) error {
	var buf bytes.Buffer

	err := insecurecleartextkeyset.Write(k.handle, keyset.NewJSONWriter(&buf))
	if err != nil {
		return err
	}

	return os.WriteFile(path, buf.Bytes(), 0o600)
}

// Rotate returns the keyset with a fresh primary key; the keys it held
// still open what they sealed.
func (k *AtRestKey) Rotate() (*AtRestKey, error) {
	manager := keyset.NewManagerFromHandle(k.handle)

	id, err := manager.Add(aead.XChaCha20Poly1305KeyTemplate())
	if err != nil {
		return nil, err
	}

	err = manager.SetPrimary(id)
	if err != nil {
		return nil, err
	}

	h, err := manager.Handle()
	if err != nil {
		return nil, err
	}

	return NewAtRestKey(h)
}

// ID marks the records sealed under the primary key.
func (k *AtRestKey) ID() []byte {
	return k.id
}

func atRestAAD(hdr *proto.ObjectHeader) []byte {
	return append(append([]byte(nil), hdr.Ref.GetHash()...), byte(hdr.Type))
}

// seal returns a payload sealed under the primary key, bound to the
// record's ref and type.
func (k *AtRestKey) seal(hdr *proto.ObjectHeader, payload []byte) ([]byte, error) {
	return k.aead.Encrypt(payload, atRestAAD(hdr))
}

// openAtRest returns a stored payload as the agent sent it: opened when the
// header says it is sealed, as is otherwise.
func openAtRest(key *AtRestKey, hdr *proto.ObjectHeader, stored []byte) ([]byte, error) {
	if len(hdr.AtRestKeyId) == 0 {
		return stored, nil
	}

	if key == nil {
		return nil, fmt.Errorf("%w: record %x", ErrAtRestKeyMissing, hdr.Ref.GetHash())
	}

	if !key.holds[string(hdr.AtRestKeyId)] {
		return nil, fmt.Errorf("%w: record %x under key %x", ErrAtRestKeyMismatch, hdr.Ref.GetHash(), hdr.AtRestKeyId)
	}

	payload, err := key.aead.Decrypt(stored, atRestAAD(hdr))
	if err != nil {
		return nil, fmt.Errorf("opening record %x sealed at rest: %w", hdr.Ref.GetHash(), err)
	}

	return payload, nil
}
