package backup

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/twcclan/goback/proto"
)

type ObjectReceiver func(*proto.Object) error

var (
	ErrNotImplemented = errors.New("the store doesn't implement this feature")
	ErrAlreadyMarked  = errors.New("object already marked")
	ErrNotFound       = errors.New("the requested object was not found")
	// ErrDanglingRef is returned when an object references objects the store
	// does not hold.
	ErrDanglingRef = errors.New("object references missing objects")
)

// References lists the refs an object points at directly: a commit's tree,
// a tree's nodes and splits, a file's parts.
func References(obj *proto.Object) []*proto.Ref {
	switch obj.Type() {
	case proto.ObjectType_COMMIT:
		return []*proto.Ref{obj.GetCommit().GetTree()}
	case proto.ObjectType_TREE:
		tree := obj.GetTree()
		refs := make([]*proto.Ref, 0, len(tree.Nodes)+len(tree.Splits))
		for _, node := range tree.Nodes {
			if node.Ref != nil {
				refs = append(refs, node.Ref)
			}
		}
		return append(refs, tree.Splits...)
	case proto.ObjectType_FILE:
		parts := obj.GetFile().GetParts()
		refs := make([]*proto.Ref, 0, len(parts))
		for _, part := range parts {
			refs = append(refs, part.Ref)
		}
		return refs
	}

	return nil
}

// CheckReferences fails with ErrDanglingRef when the store lacks any object
// the given one references. Indexes call it before accepting a Put so a
// stored tree always has its children.
func CheckReferences(ctx context.Context, store ObjectStore, obj *proto.Object) error {
	for _, ref := range References(obj) {
		ok, err := store.Has(ctx, ref)
		if err != nil {
			return err
		}

		if !ok {
			return fmt.Errorf("%w: %s %x needs %x", ErrDanglingRef, obj.Type(), obj.Ref().Hash, ref.Hash)
		}
	}

	return nil
}

//go:generate go run github.com/vektra/mockery/v2 --testonly --inpackage --name ObjectStore
type ObjectStore interface {
	Put(context.Context, *proto.Object) error
	Get(context.Context, *proto.Ref) (*proto.Object, error)
	Delete(context.Context, *proto.Ref) error
	Walk(context.Context, bool, proto.ObjectType, ObjectReceiver) error
	Has(context.Context, *proto.Ref) (bool, error)
}

type Counter interface {
	Count() (total uint64, unique uint64, err error)
}

type Collector interface {
	io.Closer
	Mark(ref *proto.Ref) error
}
