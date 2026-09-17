package storage

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/twcclan/goback/auth"
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/presence"
	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const testSecret = "shared-secret"

// memIndex is the smallest backup.Index: objects in a map, the latest
// commit and the owning agent per set.
type memIndex struct {
	mtx        sync.Mutex
	objects    map[string]*proto.Object
	referenced map[string]bool
	owners     map[string]string
	latest     map[string]*proto.Ref

	// deny refuses BeginCommit with this reason; setID and policy are what
	// it grants otherwise
	deny   string
	setID  uint64
	policy *storekey.Policy

	filters       []*proto.PresenceFilter
	presenceScope backup.PresenceScope
	presenceSet   string
}

func newMemIndex() *memIndex {
	return &memIndex{
		objects:    map[string]*proto.Object{},
		referenced: map[string]bool{},
		owners:     map[string]string{},
		latest:     map[string]*proto.Ref{},
	}
}

func (m *memIndex) Open() error  { return nil }
func (m *memIndex) Close() error { return nil }

func (m *memIndex) Put(ctx context.Context, obj *proto.Object) error {
	err := backup.CheckReferences(ctx, m, obj)
	if err != nil {
		return err
	}

	if obj.ReceivedAtNs() == 0 {
		obj.Stamp(7, time.Now())
	}

	m.mtx.Lock()
	defer m.mtx.Unlock()

	if c := obj.GetCommit(); c != nil {
		if owner, ok := m.owners[c.BackupSet]; ok && owner != c.AgentId {
			return fmt.Errorf("%w: set %q belongs to %q", backup.ErrSetOwned, c.BackupSet, owner)
		}

		m.owners[c.BackupSet] = c.AgentId
		m.latest[c.BackupSet] = obj.Ref()
	}

	m.objects[string(obj.Ref().Hash)] = obj
	m.referenced[string(obj.Ref().Hash)] = true

	return nil
}

// gated wraps a memIndex as a backup.CommitGate.
type gated struct{ *memIndex }

func (g gated) BeginCommit(context.Context, string) (*backup.CommitGrant, error) {
	if g.deny != "" {
		return nil, fmt.Errorf("%w: %s", backup.ErrCommitDenied, g.deny)
	}

	return &backup.CommitGrant{SetID: g.setID, Policy: g.policy}, nil
}

// References implements backup.RefScope; a test unmarks an object to
// make it unreachable.
func (m *memIndex) References(ctx context.Context, ref *proto.Ref) (bool, error) {
	if _, err := auth.Require(ctx); err != nil {
		return false, err
	}

	m.mtx.Lock()
	defer m.mtx.Unlock()

	return m.referenced[string(ref.Hash)], nil
}

func (m *memIndex) unreference(ref *proto.Ref) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.referenced[string(ref.Hash)] = false
}

func (m *memIndex) Get(_ context.Context, ref *proto.Ref) (*proto.Object, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	obj, ok := m.objects[string(ref.Hash)]
	if !ok {
		return nil, backup.ErrNotFound
	}

	return obj, nil
}

func (m *memIndex) Has(_ context.Context, ref *proto.Ref) (bool, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	_, ok := m.objects[string(ref.Hash)]
	return ok, nil
}

func (m *memIndex) Delete(context.Context, *proto.Ref) error { return backup.ErrNotImplemented }
func (m *memIndex) Walk(context.Context, bool, proto.ObjectType, backup.ObjectReceiver) error {
	return backup.ErrNotImplemented
}
func (m *memIndex) ReIndex(context.Context) error { return nil }
func (m *memIndex) FileInfo(context.Context, string, string, time.Time, int) ([]*proto.TreeNode, error) {
	return nil, backup.ErrNotImplemented
}
func (m *memIndex) CommitInfo(context.Context, string, time.Time, int) ([]*proto.Commit, error) {
	return nil, backup.ErrNotImplemented
}

func (m *memIndex) LatestCommit(ctx context.Context, set string) (*proto.Ref, error) {
	if _, err := auth.Require(ctx); err != nil {
		return nil, err
	}

	m.mtx.Lock()
	defer m.mtx.Unlock()

	ref, ok := m.latest[set]
	if !ok {
		return nil, backup.ErrNotFound
	}

	return ref, nil
}

// dialer connects as an agent presenting the test secret.
type dialer func(agent string) *Client

func startServer(t *testing.T) (*memIndex, dialer) {
	t.Helper()

	index := newMemIndex()

	return index, startServerWith(t, index, nil)
}

func startServerWith(t *testing.T, index backup.Index, sessions backup.SessionStore) dialer {
	t.Helper()

	return serve(t, NewStore(index, sessions))
}

// serve runs store over bufnet behind the test secret.
func serve(t *testing.T, store *Store) dialer {
	t.Helper()

	dial := serveAs(t, store)

	return func(agent string) *Client { return dial(testSecret, agent) }
}

func serveAs(t *testing.T, store *Store) func(secret, agent string) *Client {
	t.Helper()

	listener := bufconn.Listen(1 << 20)

	remote := NewServer(store)
	serverTLS, clientTLS := testTLS(t)
	srv := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(serverTLS)),
		grpc.ChainUnaryInterceptor(auth.UnaryInterceptor(testSecret), remote.UnaryInterceptor()),
		grpc.ChainStreamInterceptor(auth.StreamInterceptor(testSecret), remote.StreamInterceptor()),
	)
	proto.RegisterStoreServer(srv, remote)

	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(srv.Stop)

	return func(secret, agent string) *Client {
		con, err := grpc.NewClient("passthrough:///bufnet",
			grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)),
			grpc.WithPerRPCCredentials(auth.Credentials{Secret: secret, AgentID: agent}),
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = con.Close() })

		return &Client{store: proto.NewStoreClient(con)}
	}
}

// testTLS mints a self-signed certificate for bufnet and the loopback
// address and returns the server's configuration and a client
// configuration that trusts it.
func testTLS(t *testing.T) (server, client *tls.Config) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "goback test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"bufnet"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	pool.AddCert(cert)

	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12},
		&tls.Config{RootCAs: pool, ServerName: "bufnet", MinVersion: tls.VersionTLS12}
}

func TestRemoteRequiresSecretAndAgent(t *testing.T) {
	dial := serveAs(t, NewStore(newMemIndex(), nil))
	ctx := context.Background()

	_, err := dial("", "node-1").LatestCommit(ctx, "world")
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	_, err = dial("bogus", "node-1").LatestCommit(ctx, "world")
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	_, err = dial(testSecret, "").LatestCommit(ctx, "world")
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	_, err = dial(testSecret, "node-1").LatestCommit(ctx, "world")
	require.ErrorIs(t, err, backup.ErrNotFound)
}

func TestRemoteGetIsScoped(t *testing.T) {
	index, dial := startServer(t)
	ctx := context.Background()
	client := dial("node-1")

	blob := proto.NewObject(&proto.Blob{Data: []byte("save data")})
	require.NoError(t, client.Put(ctx, blob))
	file := proto.NewObject(&proto.File{Parts: []*proto.FilePart{{Length: 9, Ref: blob.Ref()}}})
	require.NoError(t, client.Put(ctx, file))
	tree := proto.NewObject(&proto.Tree{Nodes: []*proto.TreeNode{{Stat: &proto.FileInfo{Name: []byte("a")}, Ref: file.Ref()}}})
	require.NoError(t, client.Put(ctx, tree))

	_, err := client.Get(ctx, file.Ref())
	require.NoError(t, err)
	_, err = client.Get(ctx, tree.Ref())
	require.NoError(t, err)

	// blobs are never served, not even to their uploader
	_, err = client.Get(ctx, blob.Ref())
	require.ErrorIs(t, err, backup.ErrNotFound)

	objects, err := client.GetTree(ctx, tree.Ref(), 1)
	require.NoError(t, err)
	require.Len(t, objects, 1)

	// an object nothing references looks exactly like a missing one
	index.unreference(tree.Ref())
	_, err = client.Get(ctx, tree.Ref())
	require.ErrorIs(t, err, backup.ErrNotFound)
	_, err = client.GetTree(ctx, tree.Ref(), 1)
	require.ErrorIs(t, err, backup.ErrNotFound)
	_, err = client.Get(ctx, &proto.Ref{Hash: testHash("nothing")})
	require.ErrorIs(t, err, backup.ErrNotFound)
}

func TestRemoteReadFileStreamsParts(t *testing.T) {
	index, dial := startServer(t)
	ctx := context.Background()
	client := dial("node-1")

	var parts []*proto.FilePart
	var offset uint64
	for _, content := range []string{"one", "two", "three"} {
		blob := proto.NewObject(&proto.Blob{Data: []byte(content)})
		require.NoError(t, client.Put(ctx, blob))
		parts = append(parts, &proto.FilePart{Offset: offset, Length: uint64(len(content)), Ref: blob.Ref()})
		offset += uint64(len(content))
	}

	file := proto.NewObject(&proto.File{Parts: parts})
	require.NoError(t, client.Put(ctx, file))

	var got []string
	err := client.ReadParts(ctx, file.Ref(), []int{1}, func(i int, obj *proto.Object) error {
		got = append(got, fmt.Sprintf("%d:%s", i, obj.GetBlob().GetData()))
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"0:one", "2:three"}, got)

	// any agent of the store may stream a referenced file
	require.NoError(t, dial("node-2").ReadParts(ctx, file.Ref(), nil, func(int, *proto.Object) error { return nil }))

	index.unreference(file.Ref())
	require.ErrorIs(t, client.ReadParts(ctx, file.Ref(), nil, func(int, *proto.Object) error { return nil }), backup.ErrNotFound)

	// the trimmed calls are gone from the client
	require.ErrorIs(t, client.Delete(ctx, file.Ref()), backup.ErrNotImplemented)
	_, err = client.Has(ctx, file.Ref())
	require.ErrorIs(t, err, backup.ErrNotImplemented)
}

func TestRemoteBeginCommit(t *testing.T) {
	index := newMemIndex()
	dial := startServerWith(t, index, nil)
	ctx := context.Background()
	client := dial("node-1")

	// an index without a gate allows every commit and assigns the set later
	grant, err := client.BeginCommit(ctx, "world")
	require.NoError(t, err)
	require.Zero(t, grant.SetID)
	require.Nil(t, grant.Policy)

	gatedDial := startServerWith(t, gated{index}, nil)

	index.deny = "set is closing"
	_, err = gatedDial("node-1").BeginCommit(ctx, "world")
	require.ErrorIs(t, err, backup.ErrCommitDenied)
	require.ErrorContains(t, err, "closing")

	index.deny = ""
	index.setID = 9
	grant, err = gatedDial("node-1").BeginCommit(ctx, "world")
	require.NoError(t, err)
	require.EqualValues(t, 9, grant.SetID)
	require.Nil(t, grant.Policy, "no policy row yet")

	policy := storekey.DefaultPolicy()
	policy.Version = 4
	policy.Mode = storekey.ModeStoreKeyedAll
	policy.PresenceScope = "store"
	index.policy = &policy
	grant, err = gatedDial("node-1").BeginCommit(ctx, "world")
	require.NoError(t, err)
	require.Equal(t, &policy, grant.Policy, "the store policy travels with the grant")
}

func TestClientDialsTLS(t *testing.T) {
	index := newMemIndex()
	serverTLS, _ := testTLS(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	remote := NewServer(NewStore(index, nil))
	srv := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(serverTLS)),
		grpc.ChainUnaryInterceptor(auth.UnaryInterceptor(testSecret), remote.UnaryInterceptor()),
		grpc.ChainStreamInterceptor(auth.StreamInterceptor(testSecret), remote.StreamInterceptor()),
	)
	proto.RegisterStoreServer(srv, remote)
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(srv.Stop)

	caFile := t.TempDir() + "/ca.pem"
	require.NoError(t, os.WriteFile(caFile,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverTLS.Certificates[0].Certificate[0]}), 0o600))

	tlsConfig, err := ClientTLS(caFile)
	require.NoError(t, err)

	creds := auth.Credentials{Secret: testSecret, AgentID: "node-1"}
	client, err := NewClient(listener.Addr().String(), creds, tlsConfig)
	require.NoError(t, err)

	tree := proto.NewObject(&proto.Tree{})
	require.NoError(t, client.Put(context.Background(), tree))
	require.Len(t, index.objects, 1)

	// without the pinned authority the self-signed certificate is refused,
	// and the secret never leaves the client
	untrusted, err := NewClient(listener.Addr().String(), creds, &tls.Config{RootCAs: x509.NewCertPool(), MinVersion: tls.VersionTLS12})
	require.NoError(t, err)
	require.Error(t, untrusted.Put(context.Background(), tree))
}

func TestRemoteCommitMustMatchPrincipal(t *testing.T) {
	index, dial := startServer(t)
	ctx := context.Background()
	client := dial("node-1")

	tree := proto.NewObject(&proto.Tree{})
	require.NoError(t, client.Put(ctx, tree))

	put := func(c *Client, commit *proto.Commit) error {
		commit.Tree = tree.Ref()
		commit.Timestamp = time.Now().Unix()
		return c.Put(ctx, proto.NewObject(commit))
	}

	require.Equal(t, codes.PermissionDenied, status.Code(put(client, &proto.Commit{AgentId: "node-2", BackupSet: "world"})), "other agent")
	require.Equal(t, codes.PermissionDenied, status.Code(put(client, &proto.Commit{BackupSet: "world"})), "no agent")

	require.NoError(t, put(client, &proto.Commit{AgentId: "node-1", BackupSet: "world"}))

	latest, err := client.LatestCommit(ctx, "world")
	require.NoError(t, err)
	require.Len(t, index.latest, 1)

	obj, err := client.Get(ctx, latest)
	require.NoError(t, err)
	require.Equal(t, "node-1", obj.GetCommit().AgentId)

	// the set now belongs to node-1
	err = put(dial("node-2"), &proto.Commit{AgentId: "node-2", BackupSet: "world"})
	require.ErrorIs(t, err, backup.ErrSetOwned)
	require.NoError(t, put(dial("node-2"), &proto.Commit{AgentId: "node-2", BackupSet: "logs"}))
}

func TestRemotePutStampsCommitsAndPins(t *testing.T) {
	index, dial := startServer(t)
	ctx := context.Background()
	client := dial("node-1")

	tree := proto.NewObject(&proto.Tree{})
	require.NoError(t, client.Put(ctx, tree))

	commit := proto.NewObject(&proto.Commit{Timestamp: 1, Tree: tree.Ref(), AgentId: "node-1", BackupSet: "world", SetId: 99, ReceivedAtNs: 12345})
	before := time.Now().UnixNano()
	require.NoError(t, client.Put(ctx, commit))

	require.EqualValues(t, 7, commit.GetCommit().SetId, "the server assigns the set id, whatever the client sent")
	require.GreaterOrEqual(t, commit.GetCommit().ReceivedAtNs, before, "the server assigns the receipt time")
	require.Equal(t, tree.Ref().Hash, commit.GetCommit().Tree.Hash)

	stored, err := index.Get(ctx, commit.Ref())
	require.NoError(t, err, "the client's object hashes to the stored ref")
	require.Equal(t, commit.GetCommit().ReceivedAtNs, stored.GetCommit().ReceivedAtNs)

	pin := proto.NewObject(&proto.Pin{Target: commit.Ref(), ReceivedAtNs: 12345})
	require.NoError(t, client.Put(ctx, pin))
	require.NotZero(t, pin.GetPin().ReceivedAtNs)
	require.NotEqual(t, int64(12345), pin.GetPin().ReceivedAtNs, "the server stamps the pin")

	has, err := index.Has(ctx, pin.Ref())
	require.NoError(t, err)
	require.True(t, has)

	dangling := proto.NewObject(&proto.Pin{Target: proto.HashPayload(proto.ObjectType_BLOB, []byte("nowhere"))})
	require.ErrorIs(t, client.Put(ctx, dangling), backup.ErrDanglingRef, "a pin needs an existing target")
}

// Presence implements backup.PresenceIndex with whatever filters the test
// planted, recording the scope it was asked for.
func (m *memIndex) Presence(ctx context.Context, scope backup.PresenceScope, set string) ([]*proto.PresenceFilter, error) {
	if _, err := auth.Require(ctx); err != nil {
		return nil, err
	}

	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.presenceScope = scope
	m.presenceSet = set

	return m.filters, nil
}

func TestRemotePutFileConfirmsAssumedRefs(t *testing.T) {
	index, dial := startServer(t)
	ctx := context.Background()
	client := dial("node-1")

	a := proto.NewObject(&proto.Blob{Data: []byte("a")})
	b := proto.NewObject(&proto.Blob{Data: []byte("b")})
	require.NoError(t, client.Put(ctx, a))

	file := proto.NewObject(&proto.File{Parts: []*proto.FilePart{
		{Ref: a.Ref(), Offset: 0, Length: 1},
		{Ref: b.Ref(), Offset: 1, Length: 1},
	}})

	missing, err := client.PutFile(ctx, file, []*proto.Ref{a.Ref(), b.Ref()})
	require.NoError(t, err)
	require.Len(t, missing, 1)
	require.True(t, missing[0].Equal(b.Ref()))

	has, err := index.Has(ctx, file.Ref())
	require.NoError(t, err)
	require.False(t, has, "a file with a missing part is not stored")

	require.NoError(t, client.Put(ctx, b))

	missing, err = client.PutFile(ctx, file, []*proto.Ref{a.Ref(), b.Ref()})
	require.NoError(t, err)
	require.Empty(t, missing)

	has, err = index.Has(ctx, file.Ref())
	require.NoError(t, err)
	require.True(t, has)

	_, err = client.PutFile(ctx, a, []*proto.Ref{b.Ref()})
	require.Equal(t, codes.InvalidArgument, status.Code(err), "assumed refs belong to file objects")
}

func TestRemoteGetPresenceStreamsFiltersInPieces(t *testing.T) {
	index, dial := startServer(t)
	ctx := context.Background()

	small := presence.New(10)
	small.Set = "world"
	small.Add(testHash("one"))

	big := presence.New(2_000_000)
	big.Add(testHash("two"))
	require.Greater(t, big.Size(), 2*presencePiece, "spans several messages")

	index.filters = []*proto.PresenceFilter{small.Proto(), big.Proto()}

	filters, err := dial("node-1").Presence(ctx, "world")
	require.NoError(t, err)
	require.Len(t, filters, 2)
	require.Equal(t, "world", filters[0].Set)
	require.Equal(t, big.Size(), filters[1].Size())
	require.True(t, filters[0].Test(testHash("one")))
	require.True(t, filters[1].Test(testHash("two")))
	require.False(t, filters.Test(testHash("three")))
	require.Equal(t, backup.PresenceStore, index.presenceScope, "the server's scope, not the client's")
	require.Equal(t, "world", index.presenceSet)

	_, err = dial("").Presence(ctx, "world")
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestRemoteGetPresenceOffServesNothing(t *testing.T) {
	index := newMemIndex()
	index.filters = []*proto.PresenceFilter{presence.New(10).Proto()}

	store := NewStore(index, nil)
	store.PresenceScope = backup.PresenceOff
	dial := serve(t, store)

	filters, err := dial("node-1").Presence(context.Background(), "world")
	require.NoError(t, err)
	require.Empty(t, filters)
}

func testHash(seed string) []byte {
	sum := sha256.Sum256([]byte(seed))
	return sum[:]
}

// StorePolicy implements backup.PolicySource with the policy the test planted.
func (m *memIndex) StorePolicy(context.Context) (*storekey.Policy, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	return m.policy, nil
}

// TestRemoteGetPresenceFollowsTheStorePolicy serves filters at the scope of
// the store policy, falling back to the server flag only for a store
// without one.
func TestRemoteGetPresenceFollowsTheStorePolicy(t *testing.T) {
	index, dial := startServer(t)
	ctx := context.Background()

	filter := presence.New(10)
	filter.Add(testHash("one"))
	index.filters = []*proto.PresenceFilter{filter.Proto()}

	index.policy = &storekey.Policy{Version: 1, Mode: storekey.ModeNone, PresenceScope: "off"}
	filters, err := dial("node-1").Presence(ctx, "world")
	require.NoError(t, err)
	require.Empty(t, filters, "the store's policy turns presence off")

	index.policy = &storekey.Policy{Version: 1, Mode: storekey.ModeNone, PresenceScope: "set"}
	filters, err = dial("node-1").Presence(ctx, "world")
	require.NoError(t, err)
	require.Len(t, filters, 1)
	require.Equal(t, backup.PresenceSet, index.presenceScope)

	index.policy = nil
	_, err = dial("node-1").Presence(ctx, "world")
	require.NoError(t, err)
	require.Equal(t, backup.PresenceStore, index.presenceScope, "no policy: the server flag")
}
