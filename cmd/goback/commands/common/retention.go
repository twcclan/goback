package common

import (
	"encoding/hex"

	"github.com/gobackio/goback/backup"
	"github.com/gobackio/goback/proto"
)

// ParseRef decodes a ref printed as hex.
func ParseRef(s string) *proto.Ref {
	hash, err := hex.DecodeString(s)
	if err != nil || len(hash) != proto.HashSize {
		Fatalf("%q is not a ref: want %d hex bytes", s, proto.HashSize)
	}

	return &proto.Ref{Hash: hash}
}

// GetRetention returns the index's retention interface or exits.
func GetRetention(index backup.Index) backup.Retention {
	ret, ok := index.(backup.Retention)
	if !ok {
		Fatalf("Index %T keeps no retention state", index)
	}

	return ret
}
