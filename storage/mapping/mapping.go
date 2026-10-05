// Package mapping turns what the store service carries into goback's own
// types and back.
package mapping

import (
	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/proto"
)

//go:generate mapper .

// mapper:generate
type Mapper interface {
	StorePolicy(in *storekey.Policy) *proto.StorePolicy
	FromStorePolicy(in *proto.StorePolicy) *storekey.Policy
}
