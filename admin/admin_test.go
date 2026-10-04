package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
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

const token = "operator-secret"

type harness struct {
	t      *testing.T
	server *admin.Server
	http   *httptest.Server
	now    time.Time
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

	h := &harness{t: t, now: now}
	h.server = &admin.Server{
		Index:      x,
		RetireJob:  func(context.Context) (int, error) { return 3, nil },
		CollectJob: func(context.Context) (string, error) { return "GC generation 1", nil },
		Now:        func() time.Time { return h.now },
	}

	h.http = httptest.NewUnstartedServer(admin.Handler(token, h.server))
	h.http.Config = admin.NewHTTPServer(h.http.Config.Handler)
	h.http.Start()
	t.Cleanup(h.http.Close)

	return h
}

// call performs a REST request with the token and decodes the JSON reply.
func (h *harness) call(method, path string, body interface{}, out gproto.Message) int {
	h.t.Helper()

	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(h.t, err)
		payload = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, h.http.URL+path, payload)
	require.NoError(h.t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(h.t, err)
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	require.NoError(h.t, err)

	if out != nil && resp.StatusCode == http.StatusOK {
		require.NoError(h.t, protojson.Unmarshal(data, out), string(data))
	}

	return resp.StatusCode
}

func TestAdminRequiresToken(t *testing.T) {
	h := newHarness(t)

	resp, err := http.Get(h.http.URL + "/v1/sets")
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	req, _ := http.NewRequest(http.MethodGet, h.http.URL+"/v1/sets", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// the same listener speaks gRPC, behind the same token
	con, err := grpc.NewClient(strings.TrimPrefix(h.http.URL, "http://"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = con.Close() })

	_, err = pb.NewAdminClient(con).ListSets(context.Background(), &pb.ListSetsRequest{})
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	con, err = grpc.NewClient(strings.TrimPrefix(h.http.URL, "http://"),
		grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithPerRPCCredentials(admin.Credentials(token)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = con.Close() })

	sets, err := pb.NewAdminClient(con).ListSets(context.Background(), &pb.ListSetsRequest{})
	require.NoError(t, err)
	require.Len(t, sets.Sets, 1)
	require.Equal(t, "world", sets.Sets[0].Name)
}

func TestAdminSets(t *testing.T) {
	h := newHarness(t)

	var list pb.ListSetsResponse
	require.Equal(t, http.StatusOK, h.call(http.MethodGet, "/v1/sets", nil, &list))
	require.Len(t, list.Sets, 1)
	require.Equal(t, "world", list.Sets[0].Name)
	require.Equal(t, "active", list.Sets[0].State)
	require.EqualValues(t, 3, list.Sets[0].LogicalSize)

	require.Equal(t, http.StatusOK, h.call(http.MethodDelete, "/v1/sets/world", nil, nil))
	require.Equal(t, http.StatusOK, h.call(http.MethodGet, "/v1/sets", nil, &list))
	require.Equal(t, "closing", list.Sets[0].State)

	require.Equal(t, http.StatusOK, h.call(http.MethodPost, "/v1/sets/world/undelete", nil, nil))
	require.Equal(t, http.StatusOK, h.call(http.MethodGet, "/v1/sets", nil, &list))
	require.Equal(t, "active", list.Sets[0].State)

	require.Equal(t, http.StatusNotFound, h.call(http.MethodDelete, "/v1/sets/nope", nil, nil))
}

func TestAdminStorePolicyAndJobs(t *testing.T) {
	h := newHarness(t)

	var policy pb.StorePolicy
	require.Equal(t, http.StatusOK, h.call(http.MethodGet, "/v1/policy", nil, &policy))
	require.Zero(t, policy.Version)
	require.Equal(t, "sealed", policy.Mode)
	require.Nil(t, policy.KeyAcknowledgedAt)

	body := map[string]interface{}{"mode": "none", "presence_scope": "store", "acknowledge_key": true}
	require.Equal(t, http.StatusOK, h.call(http.MethodPut, "/v1/policy", body, &policy))
	require.EqualValues(t, 1, policy.Version)
	require.Equal(t, "none", policy.Mode)
	require.Equal(t, "store", policy.PresenceScope)
	require.NotNil(t, policy.KeyAcknowledgedAt)
	acknowledged := policy.KeyAcknowledgedAt.AsTime()
	require.True(t, acknowledged.Equal(h.now))

	h.now = h.now.Add(time.Hour)
	body["acknowledge_key"] = false
	body["mode"] = "sealed"
	require.Equal(t, http.StatusOK, h.call(http.MethodPut, "/v1/policy", body, &policy))
	require.EqualValues(t, 2, policy.Version)
	require.True(t, policy.KeyAcknowledgedAt.AsTime().Equal(acknowledged), "the acknowledgement is recorded once")

	body["mode"] = "rot13"
	require.Equal(t, http.StatusBadRequest, h.call(http.MethodPut, "/v1/policy", body, nil))
	body["mode"] = "sealed"
	body["presence_scope"] = "everyone"
	require.Equal(t, http.StatusBadRequest, h.call(http.MethodPut, "/v1/policy", body, nil))

	var retired pb.RetireResponse
	require.Equal(t, http.StatusOK, h.call(http.MethodPost, "/v1/jobs/retire", nil, &retired))
	require.EqualValues(t, 3, retired.Retired)

	var collected pb.CollectGarbageResponse
	require.Equal(t, http.StatusOK, h.call(http.MethodPost, "/v1/jobs/gc", nil, &collected))
	require.Equal(t, "GC generation 1", collected.Report)

	h.server.CollectJob = nil
	require.Equal(t, http.StatusNotImplemented, h.call(http.MethodPost, "/v1/jobs/gc", nil, nil))
}

func TestRetentionSurface(t *testing.T) {
	h := newHarness(t)

	var ret pb.Retention
	require.Equal(t, http.StatusOK, h.call(http.MethodGet, "/v1/retention", nil, &ret))
	require.Nil(t, ret.Policy, "no default was stored")
	require.EqualValues(t, 1, ret.Effective.KeepLast)
	require.EqualValues(t, 14, ret.TrashDays)

	require.Equal(t, http.StatusOK, h.call(http.MethodPut, "/v1/sets/world/retention", map[string]interface{}{"policy": map[string]interface{}{"keep_last": 3, "keep_within": 3600}}, &ret))
	require.Equal(t, "world", ret.Set)
	require.EqualValues(t, 3, ret.Policy.KeepLast)
	require.EqualValues(t, 3600, ret.Policy.KeepWithin)
	require.EqualValues(t, 3, ret.Effective.KeepLast)

	require.Equal(t, http.StatusOK, h.call(http.MethodGet, "/v1/sets/world/retention", nil, &ret))
	require.EqualValues(t, 3, ret.Policy.KeepLast)

	require.Equal(t, http.StatusOK, h.call(http.MethodPut, "/v1/retention", map[string]interface{}{"policy": map[string]interface{}{"keep_last": 2}, "trash_days": 7}, &ret))
	require.EqualValues(t, 2, ret.Policy.KeepLast)
	require.EqualValues(t, 7, ret.TrashDays)

	require.Equal(t, http.StatusOK, h.call(http.MethodPut, "/v1/sets/world/retention", map[string]interface{}{"inherit": true}, &ret))
	require.Nil(t, ret.Policy)
	require.EqualValues(t, 2, ret.Effective.KeepLast, "the set inherits the new default")

	require.Equal(t, http.StatusBadRequest, h.call(http.MethodPut, "/v1/retention", map[string]interface{}{"trash_days": -1}, nil))
	require.Equal(t, http.StatusNotFound, h.call(http.MethodGet, "/v1/sets/nowhere/retention", nil, nil))
}
