package backup

import (
	"context"
	"errors"
	"io"

	"github.com/twcclan/goback/proto"
)

type ObjectReceiver func(*proto.Object) error

var (
	ErrNotImplemented             = errors.New("the store doesn't implement this feature")
	ErrAlreadyMarked              = errors.New("object already marked")
	ErrNotFound                   = errors.New("the requested object was not found")
	ErrUnhandledTransactionStatus = errors.New("the status of the supplied transaction cannot be handled")
	ErrRollback                   = errors.New("transaction rolled back by request")
)

//go:generate go run github.com/vektra/mockery/v2 --testonly --inpackage --name ObjectStore
type ObjectStore interface {
	Put(context.Context, *proto.Object) error
	Get(context.Context, *proto.Ref) (*proto.Object, error)
	Delete(context.Context, *proto.Ref) error
	Walk(context.Context, bool, proto.ObjectType, ObjectReceiver) error
	Has(context.Context, *proto.Ref) (bool, error)
}

type Transactioner interface {
	ObjectStore
	Transaction(ctx context.Context, tx *proto.Transaction) (Transaction, error)
}

type Transaction interface {
	// Put adds an object to be stored within this transaction
	Put(ctx context.Context, object *proto.Object) error

	// Prepare should ensure that all data is safely stored and can be committed.
	//	No data can be lost after Prepare returns with a nil error
	Prepare(ctx context.Context) error

	// Commit finalizes a prepared transaction. All data should be readable after
	// 	the function returns with a nil error.
	Commit(ctx context.Context) error

	// Rollback aborts the transaction and deletes all data
	Rollback() error
}

type Counter interface {
	Count() (total uint64, unique uint64, err error)
}

type Collector interface {
	io.Closer
	Mark(ref *proto.Ref) error
}
