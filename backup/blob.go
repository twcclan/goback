package backup

import (
	"bytes"
	"fmt"

	"github.com/gobackio/goback/backup/storekey"
	"github.com/gobackio/goback/proto"
)

// BlobRef is the ref a chunk is stored under when it is sealed with key.
func BlobRef(key *storekey.Key, chunk []byte) *proto.Ref {
	return proto.HashPayload(proto.ObjectType_BLOB, key.Digest(chunk))
}

// SealBlob compresses a chunk and seals it under key.
func SealBlob(key *storekey.Key, chunk []byte) *proto.Sealed {
	ref := BlobRef(key, chunk)
	stored, compression := proto.Encode(chunk)

	return &proto.Sealed{
		Ref:         ref,
		Type:        proto.ObjectType_BLOB,
		Data:        key.SealBlob(stored, ref.Hash),
		Compression: compression,
		Encryption:  proto.Encryption_SEALED,
		KeyId:       key.ID(),
	}
}

// OpenBlob reverses SealBlob, refusing a blob sealed under another key or
// one whose plaintext does not name its ref.
func OpenBlob(key *storekey.Key, sealed *proto.Sealed) ([]byte, error) {
	if sealed.Encryption != proto.Encryption_SEALED || sealed.Type != proto.ObjectType_BLOB {
		return nil, fmt.Errorf("sealed %s object under %s is not a sealed blob", sealed.Type, sealed.Encryption)
	}

	if !bytes.Equal(sealed.KeyId, key.ID()) {
		return nil, fmt.Errorf("%w: blob %x is sealed under key %x", storekey.ErrWrongKey, sealed.Ref.GetHash(), sealed.KeyId)
	}

	stored, err := key.OpenBlob(sealed.Data, sealed.Ref.GetHash())
	if err != nil {
		return nil, err
	}

	chunk, err := proto.Decode(stored, sealed.Compression)
	if err != nil {
		return nil, fmt.Errorf("blob %x: %w", sealed.Ref.GetHash(), err)
	}

	if !BlobRef(key, chunk).Equal(sealed.Ref) {
		return nil, fmt.Errorf("%w: blob %x does not hash to its ref", storekey.ErrWrongKey, sealed.Ref.GetHash())
	}

	return chunk, nil
}
