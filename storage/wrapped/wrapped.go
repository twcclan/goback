package wrapped

import (
	"reflect"

	"github.com/twcclan/goback/backup"
)

type Wrapper interface {
	Unwrap() backup.ObjectStore
}

func Unwrap(store backup.ObjectStore) backup.ObjectStore {
	if s, ok := store.(Wrapper); ok {
		return s.Unwrap()
	}

	return nil
}

func As(store backup.ObjectStore, target interface{}) bool {
	if target == nil {
		panic("wrapped: target cannot be nil")
	}

	val := reflect.ValueOf(target)
	typ := val.Type()

	if typ.Kind() != reflect.Ptr || val.IsNil() {
		panic("wrapped: target must be a non-nil pointer")
	}

	if e := typ.Elem(); e.Kind() != reflect.Interface {
		panic("wrapped: *target must be interface")
	}

	targetType := typ.Elem()
	for store != nil {
		if reflect.TypeOf(store).AssignableTo(targetType) {
			val.Elem().Set(reflect.ValueOf(store))
			return true
		}

		store = Unwrap(store)
	}

	return false
}
