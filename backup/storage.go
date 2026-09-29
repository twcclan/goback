package backup

import (
	"context"
	"errors"
	"fmt"

	"github.com/twcclan/goback/proto"
)

// ObjectReceiver is called for every object a Walk visits; a non-nil error
// stops the walk.
type ObjectReceiver func(*proto.Object) error

var (
	// ErrNotImplemented is returned by a store or index that lacks the
	// requested feature.
	ErrNotImplemented = errors.New("the store doesn't implement this feature")
	// ErrNotFound is returned when nothing matches the given ref or set.
	ErrNotFound = errors.New("the requested object was not found")
	// ErrDanglingRef is returned when an object references objects the store
	// does not hold.
	ErrDanglingRef = errors.New("object references missing objects")
	// ErrSetOwned is returned for a commit into a set that another agent
	// owns.
	ErrSetOwned = errors.New("set is owned by another agent")
	// ErrSetClosed is returned for a commit into a set that is being
	// deleted.
	ErrSetClosed = errors.New("set is closed")
	// ErrTombstoned is returned when a commit or pin already has a
	// tombstone; there is no undo past that point.
	ErrTombstoned = errors.New("object is tombstoned")
	// ErrNewestCommit is returned when deleting a set's newest live commit
	// on its own.
	ErrNewestCommit = errors.New("the newest commit of a set cannot be deleted")
	// ErrPinned refuses the deletion of a commit a live pin holds.
	ErrPinned = errors.New("commit is pinned")
	// ErrCommitDenied is returned by BeginCommit when the store refuses the
	// run, with the reason.
	ErrCommitDenied = errors.New("commit refused")

	// ErrSessionLost is returned for a commit whose session lost objects it
	// had already been told were stored; backing up again stores them anew.
	ErrSessionLost = errors.New("the session lost objects it had stored")
)

// References lists the refs an object points at directly: a commit's tree,
// a tree's nodes and splits, a file's parts, a pin's target.
func References(obj *proto.Object) []*proto.Ref {
	switch obj.Type() {
	case proto.ObjectType_COMMIT:
		return []*proto.Ref{obj.GetCommit().GetTree()}
	case proto.ObjectType_PIN:
		return []*proto.Ref{obj.GetPin().GetTarget()}
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

// ObjectStore is content-addressed storage of objects by ref.
type ObjectStore interface {
	// Put stores the object.
	Put(context.Context, *proto.Object) error
	// Get returns the object at the ref, or ErrNotFound.
	Get(context.Context, *proto.Ref) (*proto.Object, error)
	// Delete removes the object at the ref.
	Delete(context.Context, *proto.Ref) error
	// Walk calls the receiver for every object of the type, or of every
	// type when it is INVALID; the bool says whether objects are decoded.
	Walk(context.Context, bool, proto.ObjectType, ObjectReceiver) error
	// Has reports whether the store holds the ref.
	Has(context.Context, *proto.Ref) (bool, error)
}

// A Locator is a store that may answer a read with where the bytes are
// rather than the bytes, for a caller that can fetch them itself.
type Locator interface {
	// Read returns the object at ref, or, in its place, where to fetch the
	// object's stored record. Exactly one of the two is set, and a
	// location is good for moments rather than minutes.
	Read(ctx context.Context, ref *proto.Ref) (*proto.Object, *proto.Location, error)
}

// Eraser deletes as an erasure: the tombstone asks garbage collection to
// rewrite the archives holding the target's objects as soon as its rules
// allow, instead of waiting for the dead ratio or the erasure bound.
type Eraser interface {
	// Erase tombstones the ref as an erasure.
	Erase(context.Context, *proto.Ref) error
}

// Counter is implemented by stores that can count their objects.
type Counter interface {
	// Count reports how many objects the store holds, in total and distinct.
	Count() (total uint64, unique uint64, err error)
}
