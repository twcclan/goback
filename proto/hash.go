package proto

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protowire"
	pb "google.golang.org/protobuf/proto"
)

// HashSize is the width in bytes of every Ref.
const HashSize = sha256.Size

var (
	// ErrRefMismatch is returned when stored bytes do not hash to the ref
	// they are filed under.
	ErrRefMismatch = errors.New("object bytes do not match ref")

	// ErrInvalidObject is wrapped by every error from canonical encoding.
	ErrInvalidObject = errors.New("invalid object")
)

func invalid(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrInvalidObject, fmt.Sprintf(format, args...))
}

// typeTag returns the domain-separation tag hashed in front of a payload.
func typeTag(t ObjectType) (string, bool) {
	switch t {
	case ObjectType_COMMIT:
		return "commit", true
	case ObjectType_TREE:
		return "tree", true
	case ObjectType_FILE:
		return "file", true
	case ObjectType_BLOB:
		return "blob", true
	case ObjectType_TOMBSTONE:
		return "tombstone", true
	default:
		return "", false
	}
}

// HashPayload computes the ref of a payload of the given type as
// SHA-256(tag || " " || decimal(len(payload)) || 0x00 || payload).
func HashPayload(t ObjectType, payload []byte) *Ref {
	tag, ok := typeTag(t)
	if !ok {
		panic(fmt.Sprintf("no hash tag for object type %s", t))
	}

	h := sha256.New()
	h.Write([]byte(tag))
	h.Write([]byte{' '})
	h.Write([]byte(strconv.Itoa(len(payload))))
	h.Write([]byte{0})
	h.Write(payload)

	return &Ref{Hash: h.Sum(nil)}
}

// TombstoneRef is the ref under which a tombstone for target is stored.
func TombstoneRef(target *Ref) *Ref {
	return HashPayload(ObjectType_TOMBSTONE, target.GetHash())
}

// Equal reports whether two refs name the same object.
func (r *Ref) Equal(other *Ref) bool {
	return hmac.Equal(r.GetHash(), other.GetHash())
}

// Valid reports whether the ref has the expected width.
func (r *Ref) Valid() bool {
	return len(r.GetHash()) == HashSize
}

// Ref computes the object's ref. It panics on an object that cannot be
// canonically encoded; use Canonical to get the error instead.
func (o *Object) Ref() *Ref {
	payload, err := o.Canonical()
	if err != nil {
		panic(err)
	}

	return HashPayload(o.Type(), payload)
}

// Validate reports whether the object can be canonically encoded.
func (o *Object) Validate() error {
	_, err := o.Canonical()
	return err
}

// Canonical returns the payload that is hashed and stored for the object:
// the raw chunk for a blob, and the canonical protowire encoding of the
// inner message for everything else.
func (o *Object) Canonical() ([]byte, error) {
	if err := noUnknown(o); err != nil {
		return nil, err
	}

	switch t := o.GetObject().(type) {
	case *Object_Blob:
		if err := noUnknown(t.Blob); err != nil {
			return nil, err
		}
		return t.Blob.GetData(), nil
	case *Object_Commit:
		return canonicalCommit(t.Commit)
	case *Object_Tree:
		return canonicalTree(t.Tree)
	case *Object_File:
		return canonicalFile(t.File)
	default:
		return nil, invalid("empty object")
	}
}

// NewObjectFromPayload decodes a stored payload of the given type.
func NewObjectFromPayload(payload []byte, t ObjectType) (*Object, error) {
	switch t {
	case ObjectType_BLOB:
		return NewObject(&Blob{Data: payload}), nil
	case ObjectType_COMMIT:
		c := new(Commit)
		return NewObject(c), pb.Unmarshal(payload, c)
	case ObjectType_TREE:
		tr := new(Tree)
		return NewObject(tr), pb.Unmarshal(payload, tr)
	case ObjectType_FILE:
		f := new(File)
		return NewObject(f), pb.Unmarshal(payload, f)
	default:
		return nil, invalid("cannot decode object type %s", t)
	}
}

// VerifyPayload decompresses stored bytes and fails with ErrRefMismatch
// unless they hash to ref under type t. It returns the decompressed payload.
func VerifyPayload(stored []byte, compression Compression, t ObjectType, ref *Ref) ([]byte, error) {
	payload, err := decode(stored, compression)
	if err != nil {
		return nil, err
	}

	if !HashPayload(t, payload).Equal(ref) {
		return nil, ErrRefMismatch
	}

	return payload, nil
}

// NewVerifiedObject decodes stored bytes after checking them against ref.
func NewVerifiedObject(stored []byte, compression Compression, t ObjectType, ref *Ref) (*Object, error) {
	payload, err := VerifyPayload(stored, compression, t, ref)
	if err != nil {
		return nil, err
	}

	return NewObjectFromPayload(payload, t)
}

func decode(stored []byte, compression Compression) ([]byte, error) {
	switch compression {
	case Compression_NONE:
		return stored, nil
	case Compression_GZIP:
		return decompressedBytes(stored)
	case Compression_ZSTD:
		return decodeZstd(stored)
	default:
		return nil, fmt.Errorf("unsupported compression %s", compression)
	}
}

func noUnknown(m pb.Message) error {
	if len(m.ProtoReflect().GetUnknown()) > 0 {
		return invalid("%s carries unknown fields", m.ProtoReflect().Descriptor().FullName())
	}

	return nil
}

func appendVarint(b []byte, num protowire.Number, v uint64) []byte {
	if v == 0 {
		return b
	}

	b = protowire.AppendTag(b, num, protowire.VarintType)
	return protowire.AppendVarint(b, v)
}

func appendBool(b []byte, num protowire.Number, v bool) []byte {
	if !v {
		return b
	}

	return appendVarint(b, num, 1)
}

func appendString(b []byte, num protowire.Number, v string) ([]byte, error) {
	if v == "" {
		return b, nil
	}

	if !utf8.ValidString(v) {
		return nil, invalid("field %d is not valid UTF-8", num)
	}

	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendString(b, v), nil
}

func appendMessage(b []byte, num protowire.Number, body []byte) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendBytes(b, body)
}

func appendRef(b []byte, num protowire.Number, ref *Ref) ([]byte, error) {
	if ref == nil {
		return nil, invalid("field %d: missing ref", num)
	}

	if err := noUnknown(ref); err != nil {
		return nil, err
	}

	if !ref.Valid() {
		return nil, invalid("field %d: ref has %d bytes, want %d", num, len(ref.Hash), HashSize)
	}

	body := protowire.AppendTag(nil, 1, protowire.BytesType)
	body = protowire.AppendBytes(body, ref.Hash)

	return appendMessage(b, num, body), nil
}

func canonicalCommit(c *Commit) ([]byte, error) {
	if err := noUnknown(c); err != nil {
		return nil, err
	}

	b := appendVarint(nil, 1, uint64(c.Timestamp))

	b, err := appendRef(b, 2, c.Tree)
	if err != nil {
		return nil, fmt.Errorf("commit tree: %w", err)
	}

	b, err = appendString(b, 3, c.BackupSet)
	if err != nil {
		return nil, err
	}

	if c.Parent != nil {
		b, err = appendRef(b, 4, c.Parent)
		if err != nil {
			return nil, fmt.Errorf("commit parent: %w", err)
		}
	}

	b, err = appendString(b, 5, c.AgentId)
	if err != nil {
		return nil, err
	}

	b = appendVarint(b, 6, uint64(c.ScanStartNs))
	b = appendBool(b, 7, c.Partial)

	return b, nil
}

func canonicalFileInfo(info *FileInfo) ([]byte, error) {
	if info == nil {
		return nil, invalid("missing file info")
	}

	if err := noUnknown(info); err != nil {
		return nil, err
	}

	if info.Name == "" {
		return nil, invalid("file info without a name")
	}

	b, err := appendString(nil, 1, info.Name)
	if err != nil {
		return nil, err
	}

	b = appendVarint(b, 2, uint64(info.Mode))

	b, err = appendString(b, 3, info.User)
	if err != nil {
		return nil, err
	}

	b, err = appendString(b, 4, info.Group)
	if err != nil {
		return nil, err
	}

	b = appendVarint(b, 6, uint64(info.Size))
	b = appendVarint(b, 8, uint64(info.MtimeNs))

	if _, ok := NodeType_name[int32(info.Type)]; !ok {
		return nil, invalid("unknown node type %d", info.Type)
	}

	b = appendVarint(b, 9, uint64(info.Type))

	if info.LinkTarget != "" && info.Type != NodeType_NODE_SYMLINK {
		return nil, invalid("link target on a %s node", info.Type)
	}

	return appendString(b, 10, info.LinkTarget)
}

func canonicalTree(t *Tree) ([]byte, error) {
	if err := noUnknown(t); err != nil {
		return nil, err
	}

	if len(t.Nodes) > 0 && len(t.Splits) > 0 {
		return nil, invalid("tree has both nodes and splits")
	}

	var b []byte
	var last string

	for i, node := range t.Nodes {
		if node == nil {
			return nil, invalid("tree node %d is empty", i)
		}

		if err := noUnknown(node); err != nil {
			return nil, err
		}

		name := node.GetStat().GetName()
		if i > 0 && name <= last {
			return nil, invalid("tree nodes not sorted or not unique at %q", name)
		}
		last = name

		body, err := canonicalFileInfo(node.Stat)
		if err != nil {
			return nil, fmt.Errorf("tree node %d: %w", i, err)
		}

		body = appendMessage(nil, 1, body)

		if node.Stat.Type == NodeType_NODE_SYMLINK {
			if node.Stat.LinkTarget == "" {
				return nil, invalid("tree node %q: symlink without a target", name)
			}

			if node.Ref != nil {
				return nil, invalid("tree node %q: symlink with a ref", name)
			}
		} else {
			body, err = appendRef(body, 2, node.Ref)
			if err != nil {
				return nil, fmt.Errorf("tree node %q: %w", name, err)
			}
		}

		b = appendMessage(b, 1, body)
	}

	for i, split := range t.Splits {
		var err error

		b, err = appendRef(b, 2, split)
		if err != nil {
			return nil, fmt.Errorf("tree split %d: %w", i, err)
		}
	}

	return b, nil
}

// InlineLimit is the largest file stored inline in its File object.
const InlineLimit = 4 << 10

func canonicalFile(f *File) ([]byte, error) {
	if err := noUnknown(f); err != nil {
		return nil, err
	}

	if len(f.Parts) > 0 && len(f.Splits) > 0 {
		return nil, invalid("file has both parts and splits")
	}

	if len(f.Inline) > 0 && (len(f.Parts) > 0 || len(f.Splits) > 0) {
		return nil, invalid("file has inline content and parts")
	}

	if len(f.Inline) > InlineLimit {
		return nil, invalid("inline content of %d bytes exceeds %d", len(f.Inline), InlineLimit)
	}

	if _, ok := Chunker_name[int32(f.Chunker)]; !ok {
		return nil, invalid("unknown chunker %d", f.Chunker)
	}

	var b []byte
	var next uint64

	for i, part := range f.Parts {
		if part == nil {
			return nil, invalid("file part %d is empty", i)
		}

		if err := noUnknown(part); err != nil {
			return nil, err
		}

		if part.Offset != next {
			return nil, invalid("file part %d starts at %d, want %d", i, part.Offset, next)
		}

		if part.Length == 0 {
			return nil, invalid("file part %d is empty", i)
		}

		next += part.Length

		body := appendVarint(nil, 1, part.Offset)
		body = appendVarint(body, 2, part.Length)

		body, err := appendRef(body, 3, part.Ref)
		if err != nil {
			return nil, fmt.Errorf("file part %d: %w", i, err)
		}

		b = appendMessage(b, 1, body)
	}

	for i, split := range f.Splits {
		var err error

		b, err = appendRef(b, 2, split)
		if err != nil {
			return nil, fmt.Errorf("file split %d: %w", i, err)
		}
	}

	b = appendVarint(b, 3, uint64(f.Chunker))

	if len(f.Inline) > 0 {
		b = protowire.AppendTag(b, 4, protowire.BytesType)
		b = protowire.AppendBytes(b, f.Inline)
	}

	return b, nil
}

// CanonicalEqual reports whether an already encoded payload is canonical,
// which holds when re-encoding its decoded form gives the same bytes.
func CanonicalEqual(payload []byte, t ObjectType) (bool, error) {
	obj, err := NewObjectFromPayload(payload, t)
	if err != nil {
		return false, err
	}

	canonical, err := obj.Canonical()
	if err != nil {
		return false, err
	}

	return bytes.Equal(payload, canonical), nil
}
