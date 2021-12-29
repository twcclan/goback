package backup

import (
	"context"
	"errors"
	"io"

	"github.com/twcclan/goback/proto"
)

type ObjectReceiver func(*proto.Object) error

var (
	ErrNotImplemented = errors.New("the store doesn't implement this feature")
	ErrAlreadyMarked  = errors.New("object already marked")
	ErrNotFound       = errors.New("the requested object was not found")
)

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
