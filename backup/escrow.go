package backup

import (
	"context"
	"errors"
)

var (
	// ErrOtherKeyEscrowed is returned when a store is given the escrowed
	// copy of a store key other than the one it already keeps.
	ErrOtherKeyEscrowed = errors.New("this owner already keeps the escrowed copy of another store key")
	// ErrInvalidEscrow is returned for an escrowed key that is not an
	// armored age file of a sensible size, or has no valid key id or owner.
	ErrInvalidEscrow = errors.New("not an escrowed store key")
)

// EscrowedKey is a store key escrowed under a passphrase, as goback key
// escrow prints it, with the id of the key inside.
type EscrowedKey struct {
	KeyID    string
	Escrowed []byte
}

// KeyEscrow keeps escrowed store keys beside the store's data, so a key
// survives anything the data survives. The store cannot open them. Keys
// are kept per owner; a store with one owner uses the empty one.
type KeyEscrow interface {
	// PutEscrowedKey keeps one escrowed copy for owner, once: nothing kept
	// is ever replaced. An owner keeps copies of one store key only.
	PutEscrowedKey(ctx context.Context, owner string, key EscrowedKey) error
	// EscrowedKeys returns every copy kept for owner.
	EscrowedKeys(ctx context.Context, owner string) ([]EscrowedKey, error)
}
