// Package master derives store keys from one master key file, for a
// hoster who wants one secret for a fleet of stores.
package master

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/twcclan/goback/backup/storekey"

	"github.com/tink-crypto/tink-go/v2/daead"
	"github.com/tink-crypto/tink-go/v2/keyderivation"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"github.com/tink-crypto/tink-go/v2/prf"
	tinkpb "github.com/tink-crypto/tink-go/v2/proto/tink_go_proto"
)

const kind = "goback master key"

// Key derives the store keys: the same master and name always give the
// same key, and no derived key reveals the master or another store's key.
type Key struct {
	seal, ref *keyset.Handle
}

type file struct {
	Kind string          `json:"kind"`
	Seal json.RawMessage `json:"seal"`
	Ref  json.RawMessage `json:"ref"`
}

// Generate makes a fresh master key.
func Generate() (*Key, error) {
	seal, err := deriver(daead.AESSIVKeyTemplate())
	if err != nil {
		return nil, err
	}

	ref, err := deriver(prf.HMACSHA256PRFKeyTemplate())
	if err != nil {
		return nil, err
	}

	return &Key{seal: seal, ref: ref}, nil
}

func deriver(derived *tinkpb.KeyTemplate) (*keyset.Handle, error) {
	template, err := keyderivation.CreatePRFBasedKeyTemplate(prf.HKDFSHA256PRFKeyTemplate(), derived)
	if err != nil {
		return nil, err
	}

	return keyset.NewHandle(template)
}

// Load reads a master key file written by Save.
func Load(path string) (*Key, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var f file

	err = json.Unmarshal(data, &f)
	if err != nil {
		return nil, fmt.Errorf("master key %s: %w", path, err)
	}

	if f.Kind != kind {
		return nil, fmt.Errorf("master key %s: not a master key file (kind %q)", path, f.Kind)
	}

	seal, err := storekey.ReadKeyset(f.Seal)
	if err != nil {
		return nil, fmt.Errorf("master key %s: %w", path, err)
	}

	ref, err := storekey.ReadKeyset(f.Ref)
	if err != nil {
		return nil, fmt.Errorf("master key %s: %w", path, err)
	}

	return &Key{seal: seal, ref: ref}, nil
}

// Save writes the master key file, readable by its owner only.
func (m *Key) Save(path string) error {
	seal, err := storekey.WriteKeyset(m.seal)
	if err != nil {
		return err
	}

	ref, err := storekey.WriteKeyset(m.ref)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(file{Kind: kind, Seal: seal, Ref: ref}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// Derive returns the key of the named store.
func (m *Key) Derive(name string) (*storekey.Key, error) {
	if name == "" {
		return nil, errors.New("a derived key needs a name")
	}

	seal, err := derive(m.seal, name)
	if err != nil {
		return nil, err
	}

	ref, err := derive(m.ref, name)
	if err != nil {
		return nil, err
	}

	return storekey.New(name, storekey.DefaultPolicy(), seal, ref)
}

func derive(h *keyset.Handle, name string) (*keyset.Handle, error) {
	d, err := keyderivation.New(h)
	if err != nil {
		return nil, err
	}

	return d.DeriveKeyset([]byte(name))
}
