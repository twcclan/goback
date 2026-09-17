package wrapped

import (
	"github.com/twcclan/goback/backup"
)

// Wrapper is a store layered over another store.
type Wrapper interface {
	Unwrap() backup.ObjectStore
}

// Unwrap returns the store beneath store, nil when it wraps none.
func Unwrap(store backup.ObjectStore) backup.ObjectStore {
	if s, ok := store.(Wrapper); ok {
		return s.Unwrap()
	}

	return nil
}
