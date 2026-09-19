// Package crypt is the store key's cryptography over raw key material:
// the AEAD, the key id, the deterministic tokens of name-like fields, the
// sealed part keys of a file and passphrase escrow. It carries none of
// the object format, so a holder that only reads names — the console's
// WebAssembly module — can take it without taking protobuf along.
package crypt

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	// KeySize is the width of the store master key and of every blob key.
	KeySize = 32
	// IDSize is the width of a key id.
	IDSize = 8
	// NonceSize is the width of every nonce here.
	NonceSize = chacha20poly1305.NonceSizeX
	// TagSize is the width every sealing adds.
	TagSize = chacha20poly1305.Overhead
)

// ErrWrongKey is returned when a token or sealed blob does not open under
// the given key.
var ErrWrongKey = errors.New("wrong store key or corrupt ciphertext")

// Seal encrypts plaintext under key, appending to dst, with ad bound.
func Seal(key, dst, nonce, plaintext, ad []byte) []byte {
	c, err := chacha20poly1305.NewX(key)
	if err != nil {
		panic(err)
	}

	return c.Seal(dst, nonce, plaintext, ad)
}

// Open reverses Seal, appending to dst.
func Open(key, dst, nonce, ciphertext, ad []byte) ([]byte, error) {
	c, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}

	return c.Open(dst, nonce, ciphertext, ad)
}

// ID is the key's identifier as it appears in object headers.
func ID(key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("goback store key id"))

	return mac.Sum(nil)[:IDSize]
}

const deriveInfo = "goback store key derive v1"

// Derive returns the key material of the named store under key as a
// master: the same master and name always give the same key, and no
// derived key reveals the master or another store's key.
func Derive(key []byte, name string) ([]byte, error) {
	if name == "" {
		return nil, errors.New("a derived key needs a name")
	}

	raw := make([]byte, KeySize)

	_, err := io.ReadFull(hkdf.New(sha256.New, key, []byte(deriveInfo), []byte(name)), raw)
	if err != nil {
		return nil, err
	}

	return raw, nil
}

// BlobKey derives the key a chunk is sealed under when it is sealed with
// the store key rather than with its own content.
func BlobKey(key, plaintext []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("blob"))
	mac.Write(plaintext)

	return mac.Sum(nil)
}

// Field names the FileInfo fields that are sealed.
type Field string

const (
	// FieldName is the entry name.
	FieldName Field = "name"
	// FieldUser is the owning user.
	FieldUser Field = "user"
	// FieldGroup is the owning group.
	FieldGroup Field = "group"
	// FieldTarget is a symlink's target.
	FieldTarget Field = "link_target"
)

// SealField makes the deterministic token of a name-like field within its
// parent directory: a synthetic nonce from the key, parent token, field
// and plaintext, then the AEAD under that nonce with the parent bound as
// associated data. Equal names in one directory give equal tokens; the
// same name elsewhere gives a different one. An empty value stays empty.
func SealField(key, parent []byte, field Field, plaintext []byte) []byte {
	if len(plaintext) == 0 {
		return nil
	}

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("field nonce"))
	mac.Write([]byte(field))
	mac.Write(lengthPrefixed(parent))
	mac.Write(plaintext)
	nonce := mac.Sum(nil)[:NonceSize]

	token := make([]byte, NonceSize, NonceSize+len(plaintext)+TagSize)
	copy(token, nonce)

	return Seal(key, token, nonce, plaintext, fieldAD(parent, field))
}

// OpenField reverses SealField.
func OpenField(key, parent []byte, field Field, token []byte) ([]byte, error) {
	if len(token) == 0 {
		return nil, nil
	}

	if len(token) < NonceSize+TagSize {
		return nil, ErrWrongKey
	}

	plaintext, err := Open(key, nil, token[:NonceSize], token[NonceSize:], fieldAD(parent, field))
	if err != nil {
		return nil, ErrWrongKey
	}

	return plaintext, nil
}

func fieldAD(parent []byte, field Field) []byte {
	return append(lengthPrefixed(parent), field...)
}

func lengthPrefixed(b []byte) []byte {
	out := binary.BigEndian.AppendUint32(nil, uint32(len(b)))

	return append(out, b...)
}

// SealInline seals a small file's whole content under a store-keyed blob
// key and returns the ciphertext with the key, which the file's keys
// field carries.
func SealInline(key, plaintext []byte) ([]byte, []byte) {
	blobKey := BlobKey(key, plaintext)

	var nonce [NonceSize]byte

	return Seal(blobKey, nil, nonce[:], plaintext, []byte("inline")), blobKey
}

// OpenInline reverses SealInline.
func OpenInline(blobKey, ciphertext []byte) ([]byte, error) {
	if len(blobKey) != KeySize {
		return nil, ErrWrongKey
	}

	var nonce [NonceSize]byte

	plaintext, err := Open(blobKey, nil, nonce[:], ciphertext, []byte("inline"))
	if err != nil {
		return nil, ErrWrongKey
	}

	return plaintext, nil
}

// SealKeys encrypts a file's part keys under the store key, binding the
// hash of the part each key belongs to. The nonce comes from those
// hashes, so the same parts always give the same field.
func SealKeys(key []byte, hashes, parts [][]byte) ([]byte, error) {
	if len(hashes) != len(parts) {
		return nil, fmt.Errorf("%d refs for %d keys", len(hashes), len(parts))
	}

	if len(parts) == 0 {
		return nil, nil
	}

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("keys nonce"))

	plaintext := make([]byte, 0, len(parts)*KeySize)

	for i, part := range parts {
		if len(part) != KeySize {
			return nil, fmt.Errorf("part key %d has %d bytes", i, len(part))
		}

		mac.Write(hashes[i])
		plaintext = append(plaintext, part...)
	}

	nonce := mac.Sum(nil)[:NonceSize]

	out := make([]byte, NonceSize, NonceSize+len(plaintext)+TagSize)
	copy(out, nonce)

	return Seal(key, out, nonce, plaintext, []byte("keys")), nil
}

// OpenKeys decrypts a file's part keys and returns one key per part.
func OpenKeys(key, sealed []byte) ([][]byte, error) {
	if len(sealed) == 0 {
		return nil, nil
	}

	if len(sealed) < NonceSize+TagSize {
		return nil, ErrWrongKey
	}

	plaintext, err := Open(key, nil, sealed[:NonceSize], sealed[NonceSize:], []byte("keys"))
	if err != nil {
		return nil, ErrWrongKey
	}

	if len(plaintext)%KeySize != 0 {
		return nil, ErrWrongKey
	}

	parts := make([][]byte, len(plaintext)/KeySize)
	for i := range parts {
		parts[i] = plaintext[i*KeySize : (i+1)*KeySize]
	}

	return parts, nil
}

const (
	escrowMagic  = "goback-escrow-v1"
	escrowTime   = 3
	escrowMemory = 64 << 10
	escrowSalt   = 16
)

// Escrow wraps the key under a passphrase with a memory-hard KDF, so the
// key's owner can keep it where the server cannot open it. The store name
// is bound, so a wrapped key only opens under the store it belongs to.
func Escrow(key []byte, name, passphrase string) ([]byte, error) {
	salt := make([]byte, escrowSalt)

	_, err := rand.Read(salt)
	if err != nil {
		return nil, err
	}

	wrapping := argon2.IDKey([]byte(passphrase), salt, escrowTime, escrowMemory, 1, KeySize)

	nonce := make([]byte, NonceSize)

	_, err = rand.Read(nonce)
	if err != nil {
		return nil, err
	}

	out := append([]byte(escrowMagic), salt...)
	out = append(out, nonce...)

	return Seal(wrapping, out, nonce, key, []byte(escrowMagic+name)), nil
}

// Recover unwraps an escrowed key with its passphrase and the name of the
// store it was escrowed for.
func Recover(name string, escrowed []byte, passphrase string) ([]byte, error) {
	header := len(escrowMagic) + escrowSalt + NonceSize
	if len(escrowed) < header+KeySize+TagSize || string(escrowed[:len(escrowMagic)]) != escrowMagic {
		return nil, errors.New("not an escrowed store key")
	}

	salt := escrowed[len(escrowMagic) : len(escrowMagic)+escrowSalt]
	nonce := escrowed[len(escrowMagic)+escrowSalt : header]
	wrapping := argon2.IDKey([]byte(passphrase), salt, escrowTime, escrowMemory, 1, KeySize)

	raw, err := Open(wrapping, nil, nonce, escrowed[header:], []byte(escrowMagic+name))
	if err != nil {
		return nil, errors.New("wrong passphrase or name")
	}

	return raw, nil
}
