package common

import (
	"encoding/hex"
	"log"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
)

// ParseRef decodes a ref printed as hex.
func ParseRef(s string) *proto.Ref {
	hash, err := hex.DecodeString(s)
	if err != nil || len(hash) != proto.HashSize {
		log.Fatalf("%q is not a ref: want %d hex bytes", s, proto.HashSize)
	}

	return &proto.Ref{Hash: hash}
}

// GetRetention returns the index's retention interface or exits.
func GetRetention(index backup.Index) backup.Retention {
	ret, ok := index.(backup.Retention)
	if !ok {
		log.Fatalf("Index %T keeps no retention state", index)
	}

	return ret
}
