package pack

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/twcclan/goback/backup"

	"filippo.io/age/armor"
)

const (
	escrowDir = "keys"
	// soleOwner stands for the empty owner; it is no valid key id.
	soleOwner = "store"
	escrowExt = ".age"
	// maxEscrow is far above what an escrowed key file takes up.
	maxEscrow = 64 << 10
)

var _ backup.KeyEscrow = (*PackStorage)(nil)

// PutEscrowedKey implements backup.KeyEscrow. Each copy is a file of its
// own under keys/<owner>/<key id>/, named by its content.
func (ps *PackStorage) PutEscrowedKey(ctx context.Context, owner string, key backup.EscrowedKey) error {
	dir, err := ownerDir(owner)
	if err != nil {
		return err
	}

	err = validEscrow(key)
	if err != nil {
		return err
	}

	ps.escrowMtx.Lock()
	defer ps.escrowMtx.Unlock()

	kept, err := ps.EscrowedKeys(ctx, owner)
	if err != nil {
		return err
	}

	for _, k := range kept {
		if k.KeyID != key.KeyID {
			return fmt.Errorf("%w: it keeps key %s, not %s", backup.ErrOtherKeyEscrowed, k.KeyID, key.KeyID)
		}

		if bytes.Equal(k.Escrowed, key.Escrowed) {
			return nil
		}
	}

	sum := sha256.Sum256(key.Escrowed)

	f, err := ps.storage.Create(path.Join(dir, key.KeyID, hex.EncodeToString(sum[:8])+escrowExt))
	if err != nil {
		return err
	}

	_, err = f.Write(key.Escrowed)
	if err != nil {
		_ = f.Close()
		return err
	}

	return f.Close()
}

func ownerDir(owner string) (string, error) {
	if owner == "" {
		owner = soleOwner
	}

	for _, r := range owner {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return "", fmt.Errorf("%w: owner %q", backup.ErrInvalidEscrow, owner)
		}
	}

	return path.Join(escrowDir, owner), nil
}

func validEscrow(key backup.EscrowedKey) error {
	if _, err := hex.DecodeString(key.KeyID); err != nil || key.KeyID == "" {
		return fmt.Errorf("%w: key id %q", backup.ErrInvalidEscrow, key.KeyID)
	}

	if len(key.Escrowed) > maxEscrow || !bytes.HasPrefix(bytes.TrimSpace(key.Escrowed), []byte(armor.Header)) {
		return fmt.Errorf("%w: an armored age file of at most %d bytes is expected", backup.ErrInvalidEscrow, maxEscrow)
	}

	return nil
}

// EscrowedKeys implements backup.KeyEscrow.
func (ps *PackStorage) EscrowedKeys(_ context.Context, owner string) ([]backup.EscrowedKey, error) {
	dir, err := ownerDir(owner)
	if err != nil {
		return nil, err
	}

	names, err := ps.storage.List(escrowExt)
	if err != nil {
		return nil, err
	}

	var kept []backup.EscrowedKey

	for _, name := range names {
		name = path.Clean(strings.ReplaceAll(name, "\\", "/"))
		if path.Dir(path.Dir(name)) != dir {
			continue
		}

		escrowed, err := ps.readFile(name)
		if err != nil {
			return nil, fmt.Errorf("reading escrowed key %s: %w", name, err)
		}

		kept = append(kept, backup.EscrowedKey{KeyID: path.Base(path.Dir(name)), Escrowed: escrowed})
	}

	return kept, nil
}

func (ps *PackStorage) readFile(name string) ([]byte, error) {
	f, err := ps.storage.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return io.ReadAll(io.LimitReader(f, maxEscrow+1))
}
