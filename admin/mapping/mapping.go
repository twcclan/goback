// Package mapping turns what the index reports into the admin protos.
package mapping

import (
	"github.com/twcclan/goback/index"
	pb "github.com/twcclan/goback/proto/admin"
)

//go:generate mapper .

// mapper:generate
type Mapper interface {
	Set(in index.SetInfo) *pb.BackupSet

	// field:Version from:"Policy.Version"
	// field:Mode from:"Policy.Mode"
	// field:SizeThreshold from:"Policy.SizeThreshold"
	// field:EntropyEstimator from:"Policy.EntropyEstimator"
	// field:EntropyThreshold from:"Policy.EntropyThreshold"
	// field:PresenceScope from:"Policy.PresenceScope"
	Policy(in index.StorePolicy) *pb.StorePolicy
}
