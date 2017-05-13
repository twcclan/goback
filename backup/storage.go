package backup

import (
	"github.com/twcclan/goback/proto"
)

type ObjectReceiver func(*proto.Object) error

type ObjectStore interface {
	Put(*proto.Object) error
	Get(*proto.Ref) (*proto.Object, error)
	Delete(*proto.Ref) error
	Walk(proto.ObjectType, ObjectReceiver) error
}
