package backup

import (
	"context"

	"github.com/twcclan/goback/proto"
)

type ObjectReceiver func(*proto.ObjectHeader, *proto.Object) error

type ObjectStore interface {
	Put(context.Context, *proto.Object) error
	Get(context.Context, *proto.Ref) (*proto.Object, error)
	Delete(context.Context, *proto.Ref) error
	Walk(context.Context, bool, proto.ObjectType, ObjectReceiver) error
}
