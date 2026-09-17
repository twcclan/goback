// Package mapping turns what the index reports into the admin protos.
package mapping

import (
	"time"

	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/index"
	pb "github.com/twcclan/goback/proto/admin"

	"google.golang.org/protobuf/types/known/timestamppb"
)

//go:generate mapper .

// mapper:generate
type Mapper interface {
	// field:Id from:"ID"
	// field:AgentId from:"AgentID"
	Set(in index.SetInfo) *pb.BackupSet

	// field:Version from:"Policy.Version"
	// field:Mode from:"Policy.Mode" using:"Mode"
	// field:SizeThreshold from:"Policy.SizeThreshold"
	// field:EntropyEstimator from:"Policy.EntropyEstimator"
	// field:EntropyThreshold from:"Policy.EntropyThreshold"
	// field:PresenceScope from:"Policy.PresenceScope"
	// field:Escrow from:"Policy.Escrow"
	// field:KeyAcknowledgedAt using:"Stamp"
	Policy(in index.StorePolicy) *pb.StorePolicy
}

// Stamp is an optional time as a proto timestamp, nil for none.
func Stamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}

	return timestamppb.New(*t)
}

// Mode is a policy mode as the protos carry it.
func Mode(m storekey.Mode) string {
	return string(m)
}
