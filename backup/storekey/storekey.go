// Package storekey implements the client-held store key: per-blob keys and
// refs, sealed blobs, deterministic name tokens, the encrypted per-part key
// list of a File, and passphrase escrow. The server never sees the key.
package storekey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/twcclan/goback/backup/storekey/crypt"
	"github.com/twcclan/goback/proto"
)

const (
	// KeySize is the width of the store master key and of every blob key.
	KeySize = crypt.KeySize
	// IDSize is the width of a key id.
	IDSize = crypt.IDSize

	nonceSize = crypt.NonceSize
)

var (
	// ErrWrongKey is returned when a token or sealed blob does not open
	// under the given key.
	ErrWrongKey = crypt.ErrWrongKey
	// ErrNoKey is returned when an encrypted object is met without a key.
	ErrNoKey = errors.New("object is encrypted and no store key is loaded")
)

// Field names the FileInfo fields that are sealed.
type Field = crypt.Field

const (
	// FieldName is the entry name.
	FieldName = crypt.FieldName
	// FieldUser is the owning user.
	FieldUser = crypt.FieldUser
	// FieldGroup is the owning group.
	FieldGroup = crypt.FieldGroup
	// FieldTarget is a symlink's target.
	FieldTarget = crypt.FieldTarget
)

// Mode is the store policy's encryption mode for new writes.
type Mode string

const (
	// ModeHybrid seals files under the size threshold and chunks under the
	// entropy threshold with the store key, the rest convergently.
	ModeHybrid Mode = "hybrid"
	// ModeConvergentAll seals every chunk convergently.
	ModeConvergentAll Mode = "convergent-all"
	// ModeStoreKeyedAll seals every chunk with the store key.
	ModeStoreKeyedAll Mode = "store-keyed-all"
	// ModeNone writes in the clear.
	ModeNone Mode = "none"
	// DefaultThreshold is the hybrid size threshold in bytes when the policy
	// sets none.
	DefaultThreshold = 128 << 10
	// DefaultEntropyBits is the hybrid entropy threshold in bits per byte
	// when the policy sets none.
	DefaultEntropyBits = 7.0
	// EntropyHistogramV1 names the estimator Entropy implements.
	EntropyHistogramV1 = "histogram-v1"
)

// Policy is the store's write policy, as the agent holds it.
type Policy struct {
	Version          uint32  `json:"version"`
	Mode             Mode    `json:"mode"`
	SizeThreshold    int64   `json:"size_threshold"`
	EntropyEstimator string  `json:"entropy_estimator"`
	EntropyThreshold float64 `json:"entropy_threshold"`
	PresenceScope    string  `json:"presence_scope"`
}

// DefaultPolicy is the hybrid policy with the launch parameters.
func DefaultPolicy() Policy {
	return Policy{
		Version:          1,
		Mode:             ModeHybrid,
		SizeThreshold:    DefaultThreshold,
		EntropyEstimator: EntropyHistogramV1,
		EntropyThreshold: DefaultEntropyBits,
		PresenceScope:    "store",
	}
}

// Key is a store master key, named after its store, with the policy the
// agent writes under.
type Key struct {
	Name   string
	Policy Policy

	id  []byte
	key []byte
}

type keyFile struct {
	Name   string `json:"name"`
	KeyID  string `json:"key_id"`
	Key    string `json:"key"`
	Policy Policy `json:"policy"`
}

// Generate makes a fresh key named after its store.
func Generate(name string) (*Key, error) {
	raw := make([]byte, KeySize)

	_, err := rand.Read(raw)
	if err != nil {
		return nil, err
	}

	return FromBytes(name, raw, DefaultPolicy())
}

// FromBytes wraps raw key material.
func FromBytes(name string, raw []byte, policy Policy) (*Key, error) {
	if len(raw) != KeySize {
		return nil, fmt.Errorf("store key has %d bytes, want %d", len(raw), KeySize)
	}

	return &Key{
		Name:   name,
		Policy: policy,
		id:     crypt.ID(raw),
		key:    append([]byte(nil), raw...),
	}, nil
}

// Derive returns the key of the named store under this key as a master:
// the same master and name always give the same key, and no derived key
// reveals the master or another store's key.
func (k *Key) Derive(name string) (*Key, error) {
	raw, err := crypt.Derive(k.key, name)
	if err != nil {
		return nil, err
	}

	return FromBytes(name, raw, DefaultPolicy())
}

// Load reads a key file written by Save.
func Load(path string) (*Key, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var f keyFile

	err = json.Unmarshal(data, &f)
	if err != nil {
		return nil, fmt.Errorf("parsing store key %s: %w", path, err)
	}

	raw, err := base64.StdEncoding.DecodeString(f.Key)
	if err != nil {
		return nil, fmt.Errorf("parsing store key %s: %w", path, err)
	}

	k, err := FromBytes(f.Name, raw, f.Policy)
	if err != nil {
		return nil, err
	}

	if f.KeyID != "" && f.KeyID != k.IDString() {
		return nil, fmt.Errorf("store key %s: key id %s does not match the key", path, f.KeyID)
	}

	return k, nil
}

// Save writes the key file, readable by its owner only.
func (k *Key) Save(path string) error {
	data, err := json.MarshalIndent(keyFile{
		Name:   k.Name,
		KeyID:  k.IDString(),
		Key:    base64.StdEncoding.EncodeToString(k.key),
		Policy: k.Policy,
	}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// ID identifies the key in object headers.
func (k *Key) ID() []byte { return k.id }

// IDString is the hex form of ID.
func (k *Key) IDString() string { return fmt.Sprintf("%x", k.id) }

// Bytes returns the raw key, for escrow.
func (k *Key) Bytes() []byte { return append([]byte(nil), k.key...) }

// BlobKey derives the key a chunk is sealed under.
func (k *Key) BlobKey(mode proto.Encryption, plaintext []byte) []byte {
	switch mode {
	case proto.Encryption_STORE_KEYED:
		return crypt.BlobKey(k.key, plaintext)
	case proto.Encryption_CONVERGENT:
		return ConvergentKey(plaintext)
	default:
		panic("no blob key for plaintext")
	}
}

// ConvergentKey is the content-derived key of a public chunk.
func ConvergentKey(plaintext []byte) []byte {
	h := sha256.New()
	h.Write([]byte("blob"))
	h.Write(plaintext)

	return h.Sum(nil)
}

// RefOf is the ref of a sealed blob: the typed hash of its key.
func RefOf(blobKey []byte) *proto.Ref {
	return proto.HashPayload(proto.ObjectType_BLOB, blobKey)
}

// Choose applies the policy: which mode a chunk of a file of size gets.
func (k *Key) Choose(fileSize int64, chunk []byte) proto.Encryption {
	switch k.Policy.Mode {
	case ModeConvergentAll:
		return proto.Encryption_CONVERGENT
	case ModeStoreKeyedAll:
		return proto.Encryption_STORE_KEYED
	case ModeNone:
		return proto.Encryption_PLAINTEXT
	}

	threshold := k.Policy.SizeThreshold
	if threshold == 0 {
		threshold = DefaultThreshold
	}

	if fileSize < threshold {
		return proto.Encryption_STORE_KEYED
	}

	bits := k.Policy.EntropyThreshold
	if bits == 0 {
		bits = DefaultEntropyBits
	}

	if Entropy(chunk) >= bits {
		return proto.Encryption_CONVERGENT
	}

	return proto.Encryption_STORE_KEYED
}

// Entropy is the pinned estimator: Shannon entropy of the byte histogram
// in bits per byte. It must never change; a different estimator gets a new
// name in the policy.
func Entropy(chunk []byte) float64 {
	if len(chunk) == 0 {
		return 0
	}

	var counts [256]int
	for _, b := range chunk {
		counts[b]++
	}

	n := float64(len(chunk))
	bits := 0.0

	for _, c := range counts {
		if c == 0 {
			continue
		}

		p := float64(c) / n
		bits -= p * math.Log2(p)
	}

	return bits
}

// SealBlob compresses and encrypts a chunk under its blob key and returns
// the sealed object to store. A blob key seals exactly one plaintext, so
// the nonce is fixed; the ref and type are bound as associated data.
func (k *Key) SealBlob(mode proto.Encryption, plaintext []byte) (*proto.Sealed, []byte) {
	blobKey := k.BlobKey(mode, plaintext)
	ref := RefOf(blobKey)

	compressed, compression := proto.Encode(plaintext)

	var nonce [nonceSize]byte

	sealed := &proto.Sealed{
		Ref:         ref,
		Type:        proto.ObjectType_BLOB,
		Data:        crypt.Seal(blobKey, nil, nonce[:], compressed, blobAD(ref)),
		Compression: compression,
		Encryption:  mode,
	}

	if mode == proto.Encryption_STORE_KEYED {
		sealed.KeyId = k.id
	}

	return sealed, blobKey
}

// OpenBlob decrypts and decompresses a sealed blob with its key.
func OpenBlob(blobKey []byte, sealed *proto.Sealed) ([]byte, error) {
	if len(blobKey) != KeySize {
		return nil, ErrWrongKey
	}

	var nonce [nonceSize]byte

	compressed, err := crypt.Open(blobKey, nil, nonce[:], sealed.Data, blobAD(sealed.Ref))
	if err != nil {
		return nil, ErrWrongKey
	}

	if !RefOf(blobKey).Equal(sealed.Ref) {
		return nil, ErrWrongKey
	}

	return proto.Decode(compressed, sealed.Compression)
}

func blobAD(ref *proto.Ref) []byte {
	return append([]byte("blob"), ref.GetHash()...)
}

// SealInline seals a small file's whole content under a store-keyed blob
// key and returns the ciphertext with the key, which the File's keys
// field carries.
func (k *Key) SealInline(plaintext []byte) ([]byte, []byte) {
	return crypt.SealInline(k.key, plaintext)
}

// OpenInline reverses SealInline.
func OpenInline(blobKey, ciphertext []byte) ([]byte, error) {
	return crypt.OpenInline(blobKey, ciphertext)
}

// SealField makes the deterministic token of a name-like field within its
// parent directory. Equal names in one directory give equal tokens; the
// same name elsewhere gives a different one. An empty value stays empty.
func (k *Key) SealField(parent []byte, field Field, plaintext []byte) []byte {
	return crypt.SealField(k.key, parent, field, plaintext)
}

// OpenField reverses SealField.
func (k *Key) OpenField(parent []byte, field Field, token []byte) ([]byte, error) {
	return crypt.OpenField(k.key, parent, field, token)
}

// SealKeys encrypts a File's part keys under the store key. The nonce is
// derived from the part refs, so the same parts always give the same
// field and the File ref stays deterministic.
func (k *Key) SealKeys(refs []*proto.Ref, keys [][]byte) ([]byte, error) {
	hashes := make([][]byte, len(refs))
	for i, ref := range refs {
		hashes[i] = ref.GetHash()
	}

	return crypt.SealKeys(k.key, hashes, keys)
}

// OpenKeys decrypts a File's part keys and returns one key per part.
func (k *Key) OpenKeys(sealed []byte) ([][]byte, error) {
	return crypt.OpenKeys(k.key, sealed)
}

// Escrow wraps the key under a passphrase with a memory-hard KDF, so the
// key's owner can keep it where the server cannot open it.
func (k *Key) Escrow(passphrase string) ([]byte, error) {
	return crypt.Escrow(k.key, k.Name, passphrase)
}

// Recover unwraps an escrowed key with its passphrase.
func Recover(name string, escrowed []byte, passphrase string, policy Policy) (*Key, error) {
	raw, err := crypt.Recover(name, escrowed, passphrase)
	if err != nil {
		return nil, err
	}

	return FromBytes(name, raw, policy)
}
