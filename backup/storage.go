package backup

import (
	"context"
	"errors"

	"github.com/twcclan/goback/proto"
)

type ObjectReceiver func(*proto.Object) error

var ErrNotImplemented = errors.New("the store doesn't implement this feature")

//go:generate go run github.com/vektra/mockery/v2 --testonly --inpackage --name ObjectStore
type ObjectStore interface {
	Put(context.Context, *proto.Object) error
	Get(context.Context, *proto.Ref) (*proto.Object, error)
	Delete(context.Context, *proto.Ref) error
	Walk(context.Context, bool, proto.ObjectType, ObjectReceiver) error
	Has(context.Context, *proto.Ref) (bool, error)
}
