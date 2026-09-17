package storage

import (
	"context"
	"testing"

	"github.com/twcclan/goback/auth"
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// retentionIndex is a memIndex that records retention calls and answers
// them with a configured error.
type retentionIndex struct {
	*memIndex

	calls []string
	fail  error
	agent string
}

func (r *retentionIndex) record(ctx context.Context, call string) error {
	p, err := auth.Require(ctx)
	if err != nil {
		return err
	}

	r.agent = p.AgentID
	r.calls = append(r.calls, call)

	return r.fail
}

func (r *retentionIndex) DeleteCommit(ctx context.Context, ref *proto.Ref) error {
	return r.record(ctx, "delete")
}

func (r *retentionIndex) UndeleteCommit(ctx context.Context, ref *proto.Ref) error {
	return r.record(ctx, "undelete")
}

func (r *retentionIndex) DeleteSet(ctx context.Context, set string, erase bool) error {
	return r.record(ctx, "delete-set:"+set)
}

func (r *retentionIndex) UndeleteSet(ctx context.Context, set string) error {
	return r.record(ctx, "undelete-set:"+set)
}

func (r *retentionIndex) Unpin(ctx context.Context, pin *proto.Ref) error {
	return r.record(ctx, "unpin")
}

func (r *retentionIndex) Pins(ctx context.Context) ([]*proto.PinInfo, error) {
	err := r.record(ctx, "pins")
	if err != nil {
		return nil, err
	}

	return []*proto.PinInfo{{Ref: &proto.Ref{Hash: testHash("pin")}, Target: &proto.Ref{Hash: testHash("commit")}, ReceivedAtNs: 7}}, nil
}

func TestRemoteRetentionCallsReachTheIndex(t *testing.T) {
	index := &retentionIndex{memIndex: newMemIndex()}
	dial := startServerWith(t, index, nil)
	ctx := context.Background()
	client := dial("node-1")

	ref := &proto.Ref{Hash: testHash("commit")}

	require.NoError(t, client.DeleteCommit(ctx, ref))
	require.NoError(t, client.UndeleteCommit(ctx, ref))
	require.NoError(t, client.DeleteSet(ctx, "world", true))
	require.NoError(t, client.UndeleteSet(ctx, "world"))
	require.NoError(t, client.Unpin(ctx, ref))

	pins, err := client.Pins(ctx)
	require.NoError(t, err)
	require.Len(t, pins, 1)
	require.True(t, pins[0].Target.Equal(ref))

	require.Equal(t, []string{"delete", "undelete", "delete-set:world", "undelete-set:world", "unpin", "pins"}, index.calls)
	require.Equal(t, "node-1", index.agent, "the caller's principal reaches the index")

	index.fail = backup.ErrNewestCommit
	require.Equal(t, codes.FailedPrecondition, status.Code(client.DeleteCommit(ctx, ref)))

	index.fail = backup.ErrTombstoned
	require.Equal(t, codes.FailedPrecondition, status.Code(client.UndeleteCommit(ctx, ref)))

	index.fail = backup.ErrNotFound
	require.Equal(t, codes.NotFound, status.Code(client.Unpin(ctx, ref)))

	index.fail = auth.ErrForbidden
	require.Equal(t, codes.PermissionDenied, status.Code(client.DeleteSet(ctx, "logs", false)))
}

func TestRemoteRetentionUnimplementedWithoutState(t *testing.T) {
	_, dial := startServer(t)
	ctx := context.Background()
	client := dial("node-1")

	require.Equal(t, codes.Unimplemented, status.Code(client.DeleteCommit(ctx, &proto.Ref{Hash: testHash("commit")})))

	_, err := client.Pins(ctx)
	require.Equal(t, codes.Unimplemented, status.Code(err))
}
