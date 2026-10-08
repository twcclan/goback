package admin_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/twcclan/goback/admin"
	"github.com/twcclan/goback/auth"
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/retention"
	"github.com/twcclan/goback/index/sql"
	"github.com/twcclan/goback/proto"
	pb "github.com/twcclan/goback/proto/admin"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

type memStore struct {
	mu      sync.Mutex
	objects map[string]*proto.Object
}

func (m *memStore) Put(_ context.Context, obj *proto.Object) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[string(obj.Ref().Hash)] = obj
	return nil
}

func (m *memStore) Get(_ context.Context, ref *proto.Ref) (*proto.Object, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	obj, ok := m.objects[string(ref.Hash)]
	if !ok {
		return nil, backup.ErrNotFound
	}
	return obj, nil
}

func (m *memStore) Has(_ context.Context, ref *proto.Ref) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[string(ref.Hash)]
	return ok, nil
}

func (m *memStore) Delete(context.Context, *proto.Ref) error { return nil }
func (m *memStore) Walk(context.Context, bool, proto.ObjectType, backup.ObjectReceiver) error {
	return backup.ErrNotImplemented
}

const secret = "store-secret"

type harness struct {
	t       *testing.T
	server  *admin.Server
	address string
	admin   pb.AdminClient
	now     time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()

	store := &memStore{objects: map[string]*proto.Object{}}
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	x := sql.NewMemory(t.Name(), store)
	x.Now = func() time.Time { return now }
	keepAll := retention.KeepAll
	x.DefaultPolicy = &keepAll
	require.NoError(t, x.Open())
	t.Cleanup(func() { _ = x.Close() })

	// one commit of three inline bytes by node-1 into the set "world"
	actx := auth.WithPrincipal(ctx, &auth.Principal{AgentID: "node-1"})
	file := proto.NewObject(&proto.File{Inline: []byte("one")})
	require.NoError(t, store.Put(ctx, file))
	tree := proto.NewObject(&proto.Tree{Nodes: []*proto.TreeNode{{Stat: &proto.FileInfo{Name: []byte("a.txt"), Type: proto.NodeType_NODE_FILE, Size: 3}, Ref: file.Ref()}}})
	require.NoError(t, store.Put(ctx, tree))
	require.NoError(t, x.Put(actx, proto.NewObject(&proto.Commit{Timestamp: now.Unix(), Tree: tree.Ref(), BackupSet: "world", AgentId: "node-1"})))
	_, err := x.BuildPendingPresence(ctx)
	require.NoError(t, err)
	_, err = x.MeasureSets(ctx)
	require.NoError(t, err)

	h := &harness{t: t, now: now}
	h.server = &admin.Server{
		Index:      x,
		RetireJob:  func(context.Context) (int, error) { return 3, nil },
		CollectJob: func(context.Context) (string, error) { return "GC generation 1", nil },
		Now:        func() time.Time { return h.now },
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := grpc.NewServer(grpc.UnaryInterceptor(auth.UnaryInterceptor(secret)))
	pb.RegisterAdminServer(srv, h.server)
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(srv.Stop)

	h.address = listener.Addr().String()
	h.admin = h.client(auth.Credentials{Secret: secret, AgentID: "operator", Plaintext: true})

	return h
}

// client dials the service presenting creds, none when nil.
func (h *harness) client(creds credentials.PerRPCCredentials) pb.AdminClient {
	options := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if creds != nil {
		options = append(options, grpc.WithPerRPCCredentials(creds))
	}

	con, err := grpc.NewClient(h.address, options...)
	require.NoError(h.t, err)
	h.t.Cleanup(func() { _ = con.Close() })

	return pb.NewAdminClient(con)
}

func TestAdminRequiresTheStoreSecret(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	_, err := h.client(nil).ListSets(ctx, &pb.ListSetsRequest{})
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	_, err = h.client(auth.Credentials{Secret: "wrong", AgentID: "operator", Plaintext: true}).ListSets(ctx, &pb.ListSetsRequest{})
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	sets, err := h.admin.ListSets(ctx, &pb.ListSetsRequest{})
	require.NoError(t, err)
	require.Len(t, sets.Sets, 1)
	require.Equal(t, "world", sets.Sets[0].Name)
}

func TestAdminSets(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	list, err := h.admin.ListSets(ctx, &pb.ListSetsRequest{})
	require.NoError(t, err)
	require.Len(t, list.Sets, 1)
	require.Equal(t, "world", list.Sets[0].Name)
	require.Equal(t, "active", list.Sets[0].State)
	require.EqualValues(t, 3, list.Sets[0].LogicalSize)

	_, err = h.admin.DeleteSet(ctx, &pb.DeleteSetRequest{Name: "world"})
	require.NoError(t, err)
	list, err = h.admin.ListSets(ctx, &pb.ListSetsRequest{})
	require.NoError(t, err)
	require.Equal(t, "closing", list.Sets[0].State)

	_, err = h.admin.UndeleteSet(ctx, &pb.UndeleteSetRequest{Name: "world"})
	require.NoError(t, err)
	list, err = h.admin.ListSets(ctx, &pb.ListSetsRequest{})
	require.NoError(t, err)
	require.Equal(t, "active", list.Sets[0].State)

	_, err = h.admin.DeleteSet(ctx, &pb.DeleteSetRequest{Name: "nope"})
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestAdminStorePolicyAndJobs(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	policy, err := h.admin.GetStorePolicy(ctx, &pb.GetStorePolicyRequest{})
	require.NoError(t, err)
	require.Zero(t, policy.Version)
	require.Equal(t, "sealed", policy.Mode)
	require.Nil(t, policy.KeyAcknowledgedAt)

	req := &pb.SetStorePolicyRequest{Mode: "none", PresenceScope: "store", AcknowledgeKey: true}
	policy, err = h.admin.SetStorePolicy(ctx, req)
	require.NoError(t, err)
	require.EqualValues(t, 1, policy.Version)
	require.Equal(t, "none", policy.Mode)
	require.Equal(t, "store", policy.PresenceScope)
	require.NotNil(t, policy.KeyAcknowledgedAt)
	acknowledged := policy.KeyAcknowledgedAt.AsTime()
	require.True(t, acknowledged.Equal(h.now))

	h.now = h.now.Add(time.Hour)
	req.AcknowledgeKey = false
	req.Mode = "sealed"
	policy, err = h.admin.SetStorePolicy(ctx, req)
	require.NoError(t, err)
	require.EqualValues(t, 2, policy.Version)
	require.True(t, policy.KeyAcknowledgedAt.AsTime().Equal(acknowledged), "the acknowledgement is recorded once")

	req.Mode = "rot13"
	_, err = h.admin.SetStorePolicy(ctx, req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	req.Mode = "sealed"
	req.PresenceScope = "everyone"
	_, err = h.admin.SetStorePolicy(ctx, req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	retired, err := h.admin.Retire(ctx, &pb.RetireRequest{})
	require.NoError(t, err)
	require.EqualValues(t, 3, retired.Retired)

	collected, err := h.admin.CollectGarbage(ctx, &pb.CollectGarbageRequest{})
	require.NoError(t, err)
	require.Equal(t, "GC generation 1", collected.Report)

	h.server.CollectJob = nil
	_, err = h.admin.CollectGarbage(ctx, &pb.CollectGarbageRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestRetentionSurface(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	ret, err := h.admin.GetRetention(ctx, &pb.GetRetentionRequest{})
	require.NoError(t, err)
	require.Nil(t, ret.Policy, "no default was stored")
	require.EqualValues(t, 1, ret.Effective.KeepLast)
	require.EqualValues(t, 14, ret.TrashDays)

	ret, err = h.admin.SetRetention(ctx, &pb.SetRetentionRequest{Set: "world", Policy: &pb.RetentionPolicy{KeepLast: 3, KeepWithin: 3600}})
	require.NoError(t, err)
	require.Equal(t, "world", ret.Set)
	require.EqualValues(t, 3, ret.Policy.KeepLast)
	require.EqualValues(t, 3600, ret.Policy.KeepWithin)
	require.EqualValues(t, 3, ret.Effective.KeepLast)

	ret, err = h.admin.GetRetention(ctx, &pb.GetRetentionRequest{Set: "world"})
	require.NoError(t, err)
	require.EqualValues(t, 3, ret.Policy.KeepLast)

	ret, err = h.admin.SetRetention(ctx, &pb.SetRetentionRequest{Policy: &pb.RetentionPolicy{KeepLast: 2}, TrashDays: gproto.Int32(7)})
	require.NoError(t, err)
	require.EqualValues(t, 2, ret.Policy.KeepLast)
	require.EqualValues(t, 7, ret.TrashDays)

	ret, err = h.admin.SetRetention(ctx, &pb.SetRetentionRequest{Set: "world", Inherit: true})
	require.NoError(t, err)
	require.Nil(t, ret.Policy)
	require.EqualValues(t, 2, ret.Effective.KeepLast, "the set inherits the new default")

	_, err = h.admin.SetRetention(ctx, &pb.SetRetentionRequest{TrashDays: gproto.Int32(-1)})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = h.admin.GetRetention(ctx, &pb.GetRetentionRequest{Set: "nowhere"})
	require.Equal(t, codes.NotFound, status.Code(err))
}
