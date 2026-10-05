// Package mapping turns what the index reports into the admin protos.
package mapping

import (
	"time"

	"github.com/twcclan/goback/backup/retention"
	"github.com/twcclan/goback/backup/storekey"
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

	// field:Version from:"-"
	WritePolicy(in *pb.SetStorePolicyRequest) storekey.Policy

	// field:KeepWithin using:"Seconds"
	Retention(in retention.Policy) *pb.RetentionPolicy

	// field:KeepWithin using:"Duration"
	FromRetention(in *pb.RetentionPolicy) retention.Policy

	Bracket(in retention.Bracket) *pb.RetentionBracket
	FromBracket(in *pb.RetentionBracket) retention.Bracket
}

// Seconds is d in whole seconds, as the admin protos carry durations.
func Seconds(d time.Duration) int64 {
	return int64(d / time.Second)
}

// Duration is a count of seconds as a duration.
func Duration(seconds int64) time.Duration {
	return time.Duration(seconds) * time.Second
}
