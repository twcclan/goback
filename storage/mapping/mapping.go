// Package mapping turns what the store service carries into goback's own
// types and back.
package mapping

import (
	"github.com/gobackio/goback/backup/storekey"
	"github.com/gobackio/goback/proto"
)

//go:generate mapper .

// mapper:generate
type Mapper interface {
	StorePolicy(in *storekey.Policy) *proto.StorePolicy
	FromStorePolicy(in *proto.StorePolicy) *storekey.Policy
}
