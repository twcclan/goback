package backup

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"filippo.io/age/armor"
)

// MaxEscrow is far above what an escrowed key file takes up.
const MaxEscrow = 64 << 10

var (
	// ErrOtherKeyEscrowed is returned when a store is given the escrowed
	// copy of a store key other than the one it already keeps.
	ErrOtherKeyEscrowed = errors.New("the store already keeps the escrowed copy of another store key")
	// ErrInvalidEscrow is returned for an escrowed key that is not an
	// armored age file of a sensible size, or has no valid key id.
	ErrInvalidEscrow = errors.New("not an escrowed store key")
)

// EscrowedKey is a store key escrowed under a passphrase, as goback key
// escrow prints it, with the id of the key inside.
type EscrowedKey struct {
	KeyID    string
	Escrowed []byte
}

// Validate reports ErrInvalidEscrow unless the key has a key id and is an
// armored age file of at most MaxEscrow bytes.
func (k EscrowedKey) Validate() error {
	if _, err := hex.DecodeString(k.KeyID); err != nil || k.KeyID == "" {
		return fmt.Errorf("%w: key id %q", ErrInvalidEscrow, k.KeyID)
	}

	if len(k.Escrowed) > MaxEscrow || !bytes.HasPrefix(bytes.TrimSpace(k.Escrowed), []byte(armor.Header)) {
		return fmt.Errorf("%w: an armored age file of at most %d bytes is expected", ErrInvalidEscrow, MaxEscrow)
	}

	return nil
}

// KeyEscrow keeps a store's escrowed store key beside its data, so the
// key survives anything the data survives. The store cannot open it.
type KeyEscrow interface {
	// PutEscrowedKey keeps one escrowed copy, once: nothing kept is ever
	// replaced. A store keeps copies of one store key only.
	PutEscrowedKey(ctx context.Context, key EscrowedKey) error
	// EscrowedKeys returns every copy kept.
	EscrowedKeys(ctx context.Context) ([]EscrowedKey, error)
}
