// Package mapping turns index rows into the protos and structs the rest
// of goback speaks. It sits one package below the index because the
// generator asserts its output against this interface, and declaring it
// in the index package would be an import cycle.
package mapping

import (
	"path"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/index"
	"github.com/twcclan/goback/index/sql/ent"
	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/storage/pack"
)

//go:generate mapper .

// mapper:generate
type Mapper interface {
	// field:Timestamp method:"Unix"
	// field:ReceivedAtNs from:"ReceivedAt" method:"UnixNano"
	// field:Tree using:"Ref"
	// field:SetId from:"SetID"
	// field:BackupSet from:"Edges.Set.Name"
	// field:Parent using:"Ref"
	// field:AgentId from:"AgentID"
	Commit(in *ent.CommitRow) *proto.Commit

	// field:Stat from:"."
	// field:Ref using:"Ref"
	TreeNode(in *ent.File) *proto.TreeNode

	// field:Name from:"Path" using:"Base"
	// field:Type from:"-"
	// field:LinkTarget from:"-"
	FileInfo(in *ent.File) *proto.FileInfo

	// field:Ref using:"Ref"
	// field:Target using:"Ref"
	// field:ReceivedAtNs from:"ReceivedAt" method:"UnixNano"
	Pin(in *ent.Pin) *proto.PinInfo

	// field:Set from:"BackupSet"
	// field:Restore from:"RestoreRef" using:"Ref"
	// field:Started from:"StartedAt"
	Session(in *ent.Session) *backup.Session

	// field:Name from:"ID"
	// field:Session from:"SessionID"
	Archive(in *ent.Archive) pack.ArchiveInfo

	// field:Archive from:"ArchiveID"
	// field:Record from:"."
	Location(in *ent.Object) pack.IndexLocation

	// field:Sum from:"Ref" using:"Sum"
	// field:Offset from:"Start"
	Record(in *ent.Object) pack.IndexRecord

	// field:LogicalSize from:"-"
	Set(in *ent.Set) index.SetInfo
}

// Ref wraps a stored hash, nil for none.
func Ref(hash []byte) *proto.Ref {
	if len(hash) == 0 {
		return nil
	}

	return &proto.Ref{Hash: hash}
}

// Sum is a stored hash as an index record sum.
func Sum(hash []byte) [proto.HashSize]byte {
	var sum [proto.HashSize]byte
	copy(sum[:], hash)

	return sum
}

// Base is the name component of a stored path.
func Base(p string) []byte {
	return proto.NameFromComponent(path.Base(p))
}
