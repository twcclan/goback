// Package storekey implements the client-held store key: per-blob keys and
// refs, sealed blobs, deterministic name tokens, the encrypted per-part key
// list of a File, and passphrase escrow. The server never sees the key.
package storekey

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/twcclan/goback/proto"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	// KeySize is the width of the store master key and of every blob key.
	KeySize = 32
	// IDSize is the width of a key id.
	IDSize = 8

	nonceSize = chacha20poly1305.NonceSizeX
	tagSize   = chacha20poly1305.Overhead
)

var (
	// ErrWrongKey is returned when a token or sealed blob does not open
	// under the given key.
	ErrWrongKey = errors.New("wrong store key or corrupt ciphertext")
	// ErrNoKey is returned when an encrypted object is met without a key.
	ErrNoKey = errors.New("object is encrypted and no store key is loaded")
)

// Mode is the store policy's encryption mode for new writes.
type Mode string

const (
	ModeHybrid         Mode = "hybrid"
	ModeConvergentAll  Mode = "convergent-all"
	ModeStoreKeyedAll  Mode = "store-keyed-all"
	ModeNone           Mode = "none"
	DefaultThreshold        = 128 << 10
	DefaultEntropyBits      = 7.0
	EntropyHistogramV1      = "histogram-v1"
)

// Policy is the store's write policy, as the agent holds it.
type Policy struct {
	Version          uint32  `json:"version"`
	Mode             Mode    `json:"mode"`
	SizeThreshold    int64   `json:"size_threshold"`
	EntropyEstimator string  `json:"entropy_estimator"`
	EntropyThreshold float64 `json:"entropy_threshold"`
	PresenceScope    string  `json:"presence_scope"`
	Escrow           string  `json:"escrow"`
}

// DefaultPolicy is the hybrid policy with the launch parameters.
func DefaultPolicy() Policy {
	return Policy{
		Version:          1,
		Mode:             ModeHybrid,
		SizeThreshold:    DefaultThreshold,
		EntropyEstimator: EntropyHistogramV1,
		EntropyThreshold: DefaultEntropyBits,
		PresenceScope:    "off",
		Escrow:           "off",
	}
}

// Key is a store master key with the policy the agent writes under.
type Key struct {
	StoreID string
	Policy  Policy

	id  []byte
	key []byte
}

type keyFile struct {
	Store  string `json:"store"`
	KeyID  string `json:"key_id"`
	Key    string `json:"key"`
	Policy Policy `json:"policy"`
}

// Generate makes a fresh key for a store.
func Generate(storeID string) (*Key, error) {
	raw := make([]byte, KeySize)
	_, err := rand.Read(raw)
	if err != nil {
		return nil, err
	}

	return FromBytes(storeID, raw, DefaultPolicy())
}

// FromBytes wraps raw key material.
func FromBytes(storeID string, raw []byte, policy Policy) (*Key, error) {
	if len(raw) != KeySize {
		return nil, fmt.Errorf("store key has %d bytes, want %d", len(raw), KeySize)
	}

	mac := hmac.New(sha256.New, raw)
	mac.Write([]byte("goback store key id"))

	return &Key{
		StoreID: storeID,
		Policy:  policy,
		id:      mac.Sum(nil)[:IDSize],
		key:     append([]byte(nil), raw...),
	}, nil
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

	k, err := FromBytes(f.Store, raw, f.Policy)
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
		Store:  k.StoreID,
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

func (k *Key) cipher() *aeadKey {
	return newAEAD(k.key)
}

// BlobKey derives the key a chunk is sealed under.
func (k *Key) BlobKey(mode proto.Encryption, plaintext []byte) []byte {
	switch mode {
	case proto.Encryption_STORE_KEYED:
		mac := hmac.New(sha256.New, k.key)
		mac.Write([]byte("blob"))
		mac.Write(plaintext)
		return mac.Sum(nil)
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
	data := newAEAD(blobKey).Seal(nil, nonce[:], compressed, blobAD(ref))

	sealed := &proto.Sealed{
		Ref:         ref,
		Type:        proto.ObjectType_BLOB,
		Data:        data,
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
	compressed, err := newAEAD(blobKey).Open(nil, nonce[:], sealed.Data, blobAD(sealed.Ref))
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
	blobKey := k.BlobKey(proto.Encryption_STORE_KEYED, plaintext)

	var nonce [nonceSize]byte

	return newAEAD(blobKey).Seal(nil, nonce[:], plaintext, []byte("inline")), blobKey
}

// OpenInline reverses SealInline.
func OpenInline(blobKey, ciphertext []byte) ([]byte, error) {
	if len(blobKey) != KeySize {
		return nil, ErrWrongKey
	}

	var nonce [nonceSize]byte
	plaintext, err := newAEAD(blobKey).Open(nil, nonce[:], ciphertext, []byte("inline"))
	if err != nil {
		return nil, ErrWrongKey
	}

	return plaintext, nil
}

// Field names the FileInfo fields that are sealed.
type Field string

const (
	FieldName   Field = "name"
	FieldUser   Field = "user"
	FieldGroup  Field = "group"
	FieldTarget Field = "link_target"
)

// SealField makes the deterministic token of a name-like field within its
// parent directory: a synthetic nonce from the key, parent token, field
// and plaintext, then the AEAD under that nonce with the parent bound as
// associated data. Equal names in one directory give equal tokens; the
// same name elsewhere gives a different one. An empty value stays empty.
func (k *Key) SealField(parent []byte, field Field, plaintext []byte) []byte {
	if len(plaintext) == 0 {
		return nil
	}

	mac := hmac.New(sha256.New, k.key)
	mac.Write([]byte("field nonce"))
	mac.Write([]byte(field))
	mac.Write(lengthPrefixed(parent))
	mac.Write(plaintext)
	nonce := mac.Sum(nil)[:nonceSize]

	token := make([]byte, nonceSize, nonceSize+len(plaintext)+tagSize)
	copy(token, nonce)

	return k.cipher().Seal(token, nonce, plaintext, fieldAD(parent, field))
}

// OpenField reverses SealField.
func (k *Key) OpenField(parent []byte, field Field, token []byte) ([]byte, error) {
	if len(token) == 0 {
		return nil, nil
	}

	if len(token) < nonceSize+tagSize {
		return nil, ErrWrongKey
	}

	plaintext, err := k.cipher().Open(nil, token[:nonceSize], token[nonceSize:], fieldAD(parent, field))
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

// SealKeys encrypts a File's part keys under the store key. The nonce is
// derived from the part refs, so the same parts always give the same
// field and the File ref stays deterministic.
func (k *Key) SealKeys(refs []*proto.Ref, keys [][]byte) ([]byte, error) {
	if len(refs) != len(keys) {
		return nil, fmt.Errorf("%d refs for %d keys", len(refs), len(keys))
	}

	if len(keys) == 0 {
		return nil, nil
	}

	mac := hmac.New(sha256.New, k.key)
	mac.Write([]byte("keys nonce"))
	plaintext := make([]byte, 0, len(keys)*KeySize)
	for i, key := range keys {
		if len(key) != KeySize {
			return nil, fmt.Errorf("part key %d has %d bytes", i, len(key))
		}

		mac.Write(refs[i].GetHash())
		plaintext = append(plaintext, key...)
	}
	nonce := mac.Sum(nil)[:nonceSize]

	out := make([]byte, nonceSize, nonceSize+len(plaintext)+tagSize)
	copy(out, nonce)

	return k.cipher().Seal(out, nonce, plaintext, []byte("keys")), nil
}

// OpenKeys decrypts a File's part keys and returns one key per part.
func (k *Key) OpenKeys(sealed []byte) ([][]byte, error) {
	if len(sealed) == 0 {
		return nil, nil
	}

	if len(sealed) < nonceSize+tagSize {
		return nil, ErrWrongKey
	}

	plaintext, err := k.cipher().Open(nil, sealed[:nonceSize], sealed[nonceSize:], []byte("keys"))
	if err != nil {
		return nil, ErrWrongKey
	}

	if len(plaintext)%KeySize != 0 {
		return nil, ErrWrongKey
	}

	keys := make([][]byte, len(plaintext)/KeySize)
	for i := range keys {
		keys[i] = plaintext[i*KeySize : (i+1)*KeySize]
	}

	return keys, nil
}

const (
	escrowMagic  = "goback-escrow-v1"
	escrowTime   = 3
	escrowMemory = 64 << 10
	escrowSalt   = 16
)

// Escrow wraps the key under a passphrase with a memory-hard KDF, for a
// customer to store where the service cannot open it.
func (k *Key) Escrow(passphrase string) ([]byte, error) {
	salt := make([]byte, escrowSalt)
	_, err := rand.Read(salt)
	if err != nil {
		return nil, err
	}

	wrapping := argon2.IDKey([]byte(passphrase), salt, escrowTime, escrowMemory, 1, KeySize)

	nonce := make([]byte, nonceSize)
	_, err = rand.Read(nonce)
	if err != nil {
		return nil, err
	}

	out := append([]byte(escrowMagic), salt...)
	out = append(out, nonce...)

	return newAEAD(wrapping).Seal(out, nonce, k.key, []byte(escrowMagic+k.StoreID)), nil
}

// Recover unwraps an escrowed key with its passphrase.
func Recover(storeID string, escrowed []byte, passphrase string, policy Policy) (*Key, error) {
	header := len(escrowMagic) + escrowSalt + nonceSize
	if len(escrowed) < header+KeySize+tagSize || string(escrowed[:len(escrowMagic)]) != escrowMagic {
		return nil, errors.New("not an escrowed store key")
	}

	salt := escrowed[len(escrowMagic) : len(escrowMagic)+escrowSalt]
	nonce := escrowed[len(escrowMagic)+escrowSalt : header]
	wrapping := argon2.IDKey([]byte(passphrase), salt, escrowTime, escrowMemory, 1, KeySize)

	raw, err := newAEAD(wrapping).Open(nil, nonce, escrowed[header:], []byte(escrowMagic+storeID))
	if err != nil {
		return nil, errors.New("wrong passphrase or store id")
	}

	return FromBytes(storeID, raw, policy)
}

type aeadKey struct {
	key []byte
}

func newAEAD(key []byte) *aeadKey {
	return &aeadKey{key: key}
}

func (a *aeadKey) Seal(dst, nonce, plaintext, ad []byte) []byte {
	c, err := chacha20poly1305.NewX(a.key)
	if err != nil {
		panic(err)
	}

	return c.Seal(dst, nonce, plaintext, ad)
}

func (a *aeadKey) Open(dst, nonce, ciphertext, ad []byte) ([]byte, error) {
	c, err := chacha20poly1305.NewX(a.key)
	if err != nil {
		return nil, err
	}

	return c.Open(dst, nonce, ciphertext, ad)
}
