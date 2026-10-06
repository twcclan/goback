package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/twcclan/goback/auth"
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/proto"
	adminpb "github.com/twcclan/goback/proto/admin"
	"github.com/twcclan/goback/storage/pack"

	"github.com/stretchr/testify/require"
	"gocloud.dev/blob/fileblob"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// shapedConn delays every byte crossing it by delay in each direction and,
// with rate above zero, lets at most rate bytes a second through each way,
// the way a long link between an agent and its store does. It counts the
// bytes it receives into each of received.
type shapedConn struct {
	net.Conn
	delay    time.Duration
	rate     float64
	received []*atomic.Int64

	out  chan packet
	in   chan packet
	done chan struct{}
	once sync.Once

	sendMtx  sync.Mutex
	sendBusy time.Time

	pending []byte
	readErr error
}

// linkQueue is how much a shaped link holds back before it stops reading.
const linkQueue = 5 * time.Millisecond

type packet struct {
	data []byte
	at   time.Time
}

func shape(conn net.Conn, delay time.Duration, rate float64, received ...*atomic.Int64) *shapedConn {
	c := &shapedConn{
		Conn:     conn,
		delay:    delay,
		rate:     rate,
		received: received,
		out:      make(chan packet, 1<<14),
		in:       make(chan packet, 1<<14),
		done:     make(chan struct{}),
	}

	go c.send()
	go c.receive()

	return c
}

// release is when n bytes ready at ready have crossed a link that was
// busy until busy.
func (c *shapedConn) release(busy *time.Time, ready time.Time, n int) time.Time {
	// an idle link keeps a little credit, or every late wakeup of the
	// reader would cost the link that much of its rate
	if idle := ready.Add(-linkQueue); busy.Before(idle) {
		*busy = idle
	}

	if c.rate > 0 {
		*busy = busy.Add(time.Duration(float64(n) / c.rate * float64(time.Second)))
	}

	if busy.Before(ready) {
		return ready.Add(c.delay)
	}

	return busy.Add(c.delay)
}

func (c *shapedConn) send() {
	for {
		select {
		case p := <-c.out:
			time.Sleep(time.Until(p.at))

			if _, err := c.Conn.Write(p.data); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *shapedConn) receive() {
	defer close(c.in)

	var busy time.Time

	for {
		buf := make([]byte, 64<<10)

		n, err := c.Conn.Read(buf)
		if n > 0 {
			for _, r := range c.received {
				r.Add(int64(n))
			}
			c.in <- packet{data: buf[:n], at: c.release(&busy, time.Now(), n)}

			// a link queues little before it drops, so the sender must
			// feel the cap rather than fill an endless buffer
			time.Sleep(time.Until(busy) - linkQueue)
		}

		if err != nil {
			c.readErr = err
			return
		}
	}
}

func (c *shapedConn) Write(p []byte) (int, error) {
	c.sendMtx.Lock()
	at := c.release(&c.sendBusy, time.Now(), len(p))
	c.sendMtx.Unlock()

	select {
	case c.out <- packet{data: append([]byte(nil), p...), at: at}:
		return len(p), nil
	case <-c.done:
		return 0, net.ErrClosed
	}
}

func (c *shapedConn) Read(p []byte) (int, error) {
	if len(c.pending) == 0 {
		pkt, ok := <-c.in
		if !ok {
			if c.readErr == nil {
				return 0, io.EOF
			}

			return 0, c.readErr
		}

		time.Sleep(time.Until(pkt.at))
		c.pending = pkt.data
	}

	n := copy(p, c.pending)
	c.pending = c.pending[n:]

	return n, nil
}

func (c *shapedConn) Close() error {
	c.once.Do(func() { close(c.done) })

	return c.Conn.Close()
}

// slowPack answers every Get after delay, like a store reading its
// objects from a bucket.
type slowPack struct {
	*packIndex
	delay time.Duration
}

func (s slowPack) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	time.Sleep(s.delay)
	return s.packIndex.Get(ctx, ref)
}

// signedBucket hands out URLs into an HTTP server over the bucket's
// directory, the way Cloud Storage signs ranges of its objects.
type signedBucket struct {
	*BucketStore
	url string
}

func (s signedBucket) SignRange(_ context.Context, name string, offset, length int64, ttl time.Duration) (*proto.Location, error) {
	return &proto.Location{
		Url:     s.url + "/" + ArchiveKey(name),
		Header:  map[string]string{"Range": fmt.Sprintf("bytes=%d-%d", offset, offset+length-1)},
		Length:  length,
		Expires: timestamppb.New(time.Now().Add(ttl)),
	}, nil
}

// link is the network and bucket a benchmark restores across.
type link struct {
	rtt time.Duration
	// rate caps each connection in bytes a second, 0 for no cap
	rate float64
	// get is the bucket's latency for every read
	get time.Duration
}

func (l link) String() string {
	s := fmt.Sprintf("rtt=%s/get=%s", l.rtt, l.get)
	if l.rate > 0 {
		s += fmt.Sprintf("/cap=%dMBps", int(l.rate/(1<<20)))
	}

	return s
}

// restoreFixture is a directory of archives holding files sealed under
// one key.
type restoreFixture struct {
	dir   string
	key   *storekey.Key
	files []*proto.Ref
	sizes []int64
	total int64
}

func openPack(tb testing.TB, archives pack.ArchiveStorage) *pack.PackStorage {
	tb.Helper()

	store, err := pack.NewPackStorage(
		pack.WithArchiveStorage(archives),
		pack.WithArchiveIndex(pack.NewInMemoryIndex()),
		pack.WithLogger(slog.New(slog.DiscardHandler)),
	)
	require.NoError(tb, err)
	require.NoError(tb, store.Open())

	return store
}

func newRestoreFixture(tb testing.TB, count int, size int64, content func(data []byte, i int)) *restoreFixture {
	tb.Helper()

	key, err := storekey.Generate("bench")
	require.NoError(tb, err)

	f := &restoreFixture{dir: tb.TempDir(), key: key}

	bucket, err := fileblob.OpenBucket(f.dir, nil)
	require.NoError(tb, err)

	store := openPack(tb, NewBucketStore(bucket))

	for i := range count {
		data := make([]byte, size)
		content(data, i)

		ref, err := backup.PutFile(context.Background(), store, key, size, &readerOf{data: data})
		require.NoError(tb, err)

		f.files = append(f.files, ref)
		f.sizes = append(f.sizes, size)
		f.total += size
	}

	require.NoError(tb, store.Close())

	return f
}

func random(data []byte, i int) { rand.New(rand.NewSource(int64(i))).Read(data) }

// repeated fills data with one random MiB over and over, so the chunker
// cuts the same incompressible parts from every copy.
func repeated(data []byte, i int) {
	chunk := make([]byte, 1<<20)
	random(chunk, i)

	for at := 0; at < len(data); at += len(chunk) {
		copy(data[at:], chunk)
	}
}

type readerOf struct{ data []byte }

func (r *readerOf) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}

	n := copy(p, r.data)
	r.data = r.data[n:]

	return n, nil
}

// traffic is what a client of serveShaped received: bytes over every
// connection, bytes from the store server alone, and requests to the
// bucket.
type traffic struct {
	received, served, fetches atomic.Int64
}

// serveShaped runs a TLS store server over f's archives on loopback TCP
// and, when located, a bucket the client reads them from itself. It
// returns a client whose connections all cross l, and what it received.
func serveShaped(tb testing.TB, f *restoreFixture, l link, located bool) (*Client, *traffic) {
	tb.Helper()

	seen := &traffic{}
	dial := func(ctx context.Context, network, addr string, counters ...*atomic.Int64) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}

		return shape(conn, l.rtt/2, l.rate, append(counters, &seen.received)...), nil
	}

	bucket, err := fileblob.OpenBucket(f.dir, nil)
	require.NoError(tb, err)

	buckets := NewBucketStore(bucket)
	var archives pack.ArchiveStorage = buckets
	var httpClient *http.Client

	if located {
		files := http.FileServer(http.Dir(f.dir))
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen.fetches.Add(1)
			time.Sleep(l.get)
			files.ServeHTTP(w, r)
		}))
		server.EnableHTTP2 = true
		server.StartTLS()
		tb.Cleanup(server.Close)

		transport := server.Client().Transport.(*http.Transport).Clone()
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) { return dial(ctx, network, addr) }
		transport.ForceAttemptHTTP2 = true
		httpClient = &http.Client{Transport: transport}

		archives = signedBucket{BucketStore: buckets, url: server.URL}
	}

	packs := openPack(tb, archives)
	tb.Cleanup(func() { _ = packs.Close() })

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(tb, err)

	index := slowPack{packIndex: &packIndex{PackStorage: packs, latest: map[string]*proto.Ref{}}, delay: l.get}
	remote := NewServer(NewStore(index, nil))
	serverTLS, clientTLS := testTLS(tb)
	srv := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(serverTLS)),
		grpc.ChainUnaryInterceptor(auth.UnaryInterceptor(testSecret), remote.UnaryInterceptor()),
		grpc.ChainStreamInterceptor(auth.StreamInterceptor(testSecret), remote.StreamInterceptor()),
	)
	proto.RegisterStoreServer(srv, remote)

	go func() { _ = srv.Serve(listener) }()
	tb.Cleanup(srv.Stop)

	received := &downloads{}
	con, err := grpc.NewClient("passthrough:///"+listener.Addr().String(), append([]grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)),
		grpc.WithPerRPCCredentials(auth.Credentials{Secret: testSecret, AgentID: "bench"}),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) { return dial(ctx, "tcp", addr, &seen.served) }),
		grpc.WithStatsHandler(received),
	}, downloadWindows...)...)
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = con.Close() })

	return &Client{store: proto.NewStoreClient(con), admin: adminpb.NewAdminClient(con), HTTP: httpClient, downloads: received}, seen
}

// restoreAll restores every file of f into a new directory, workers files
// at once, and returns where.
func restoreAll(tb testing.TB, restorer *backup.Restorer, f *restoreFixture, workers int) string {
	into := tb.TempDir()

	files, ctx := errgroup.WithContext(context.Background())
	files.SetLimit(workers)

	for i, ref := range f.files {
		files.Go(func() error {
			path := filepath.Join(into, fmt.Sprintf("file%d", i))
			stat := &proto.FileInfo{Size: f.sizes[i], Mode: 0o644, MtimeNs: time.Now().UnixNano()}

			_, err := restorer.RestoreFile(ctx, path, stat, ref)

			return err
		})
	}

	require.NoError(tb, files.Wait())

	return into
}

// benchRestore restores f across l per op and reports the bytes received
// per restored byte beside the throughput.
func benchRestore(b *testing.B, f *restoreFixture, l link, located bool, workers int) {
	client, seen := serveShaped(b, f, l, located)
	restorer := &backup.Restorer{Store: client, Key: f.key, Workers: workers}

	b.SetBytes(f.total)
	b.ResetTimer()

	for range b.N {
		restoreAll(b, restorer, f, workers)
	}

	b.ReportMetric(float64(seen.received.Load())/float64(b.N)/float64(f.total), "wire/logical")
}

var benchLinks = []link{
	{rtt: 30 * time.Millisecond},
	{rtt: 30 * time.Millisecond, get: 20 * time.Millisecond},
	{rtt: 30 * time.Millisecond, get: 20 * time.Millisecond, rate: 100 << 20},
}

// BenchmarkRemoteRestore restores across a shaped link from a real store
// server, the parts served by it or read from the bucket: one large file,
// one made of a single repeated chunk, and many medium ones, eight files
// at once as on a four-core host.
func BenchmarkRemoteRestore(b *testing.B) {
	if testing.Short() {
		b.Skip("restores hundreds of megabytes")
	}

	const workers = 8

	fixtures := []struct {
		name string
		f    func() *restoreFixture
	}{
		{"large", sync.OnceValue(func() *restoreFixture { return newRestoreFixture(b, 1, 256<<20, random) })},
		{"repeated", sync.OnceValue(func() *restoreFixture { return newRestoreFixture(b, 1, 256<<20, repeated) })},
		{"medium", sync.OnceValue(func() *restoreFixture { return newRestoreFixture(b, 64, 4<<20, random) })},
	}

	for _, l := range benchLinks {
		for _, fixture := range fixtures {
			for _, mode := range []string{"proxied", "located"} {
				b.Run(fixture.name+"/"+mode+"/"+l.String(), func(b *testing.B) {
					benchRestore(b, fixture.f(), l, mode == "located", workers)
				})
			}
		}
	}
}

func TestARestoreReadsLocatedPartsFromTheBucket(t *testing.T) {
	f := newRestoreFixture(t, 3, 3<<20, random)
	repeats := newRestoreFixture(t, 1, 4<<20, repeated)

	for _, located := range []bool{false, true} {
		client, seen := serveShaped(t, f, link{}, located)
		into := restoreAll(t, &backup.Restorer{Store: client, Key: f.key, Workers: 4}, f, 4)

		for i := range f.files {
			want := make([]byte, f.sizes[i])
			random(want, i)

			got, err := os.ReadFile(filepath.Join(into, fmt.Sprintf("file%d", i)))
			require.NoError(t, err)
			require.True(t, bytes.Equal(want, got), "file %d restores whole", i)
		}

		if located {
			require.NotZero(t, seen.fetches.Load(), "the parts come from the bucket")
		} else {
			require.Zero(t, seen.fetches.Load())
		}

		require.Greater(t, client.Downloaded(), f.total*9/10, "what came from the server and the bucket is counted")
		require.LessOrEqual(t, client.Downloaded(), seen.received.Load(), "and no more than crossed the wire")

		client, seen = serveShaped(t, repeats, link{}, located)
		restoreAll(t, &backup.Restorer{Store: client, Key: repeats.key}, repeats, 1)
		require.Less(t, seen.received.Load(), repeats.total/2, "a part the file repeats crosses the wire once")
	}
}

// newTreeFixture stores a directory of dirs directories, each of subs
// directories of files files, with names as random as sealed ones, and
// returns it beside the root's ref.
func newTreeFixture(tb testing.TB, dirs, subs, files int) (*restoreFixture, *proto.Ref) {
	tb.Helper()

	f := &restoreFixture{dir: tb.TempDir()}

	bucket, err := fileblob.OpenBucket(f.dir, nil)
	require.NoError(tb, err)

	store := openPack(tb, NewBucketStore(bucket))
	rnd := rand.New(rand.NewSource(1))
	ctx := context.Background()

	name := func() []byte {
		b := make([]byte, 32)
		rnd.Read(b)

		return b
	}

	node := func(kind proto.NodeType, ref *proto.Ref) *proto.TreeNode {
		return &proto.TreeNode{
			Stat: &proto.FileInfo{Name: name(), Mode: 0o644, Size: rnd.Int63n(1 << 30), MtimeNs: rnd.Int63(), Type: kind},
			Ref:  ref,
		}
	}

	put := func(nodes []*proto.TreeNode) *proto.Ref {
		slices.SortFunc(nodes, func(a, b *proto.TreeNode) int { return bytes.Compare(a.Stat.Name, b.Stat.Name) })
		tree := proto.NewObject(&proto.Tree{Nodes: nodes})
		require.NoError(tb, store.Put(ctx, tree))

		return tree.Ref()
	}

	var top []*proto.TreeNode
	for range dirs {
		var mid []*proto.TreeNode
		for range subs {
			var leaves []*proto.TreeNode
			for range files {
				leaves = append(leaves, node(proto.NodeType_NODE_FILE, &proto.Ref{Hash: name()}))
			}

			mid = append(mid, node(proto.NodeType_NODE_DIRECTORY, put(leaves)))
		}

		top = append(top, node(proto.NodeType_NODE_DIRECTORY, put(mid)))
	}

	root := put(top)
	require.NoError(tb, store.Close())

	return f, root
}

// BenchmarkRemoteGetTree prefetches a tree of 64 directories of 16
// directories of 64 files two levels deep, as a backup does its base
// commit, the trees served by the store or read from the bucket.
func BenchmarkRemoteGetTree(b *testing.B) {
	f, root := sync.OnceValues(func() (*restoreFixture, *proto.Ref) { return newTreeFixture(b, 64, 16, 64) })()

	for _, l := range append([]link{{}}, benchLinks...) {
		for _, mode := range []string{"proxied", "located"} {
			b.Run(mode+"/"+l.String(), func(b *testing.B) {
				client, seen := serveShaped(b, f, l, mode == "located")
				b.ResetTimer()

				for range b.N {
					objects, err := client.GetTree(context.Background(), root, 2)
					require.NoError(b, err)
					require.Len(b, objects, 1+64+64*16)
				}

				b.ReportMetric(float64(seen.received.Load())/float64(b.N), "wire-B/op")
				b.ReportMetric(float64(seen.served.Load())/float64(b.N), "served-B/op")
				b.ReportMetric(float64(seen.fetches.Load())/float64(b.N), "fetches/op")
			})
		}
	}
}

func TestGetTreeReadsLocatedTreesFromTheBucket(t *testing.T) {
	f, root := newTreeFixture(t, 3, 4, 5)

	for _, located := range []bool{false, true} {
		client, seen := serveShaped(t, f, link{}, located)

		objects, err := client.GetTree(context.Background(), root, 2)
		require.NoError(t, err)
		require.Len(t, objects, 1+3+3*4)

		var dirs int
		for _, obj := range objects {
			require.NotNil(t, obj.GetTree())

			for _, node := range obj.GetTree().GetNodes() {
				if node.GetStat().IsDir() {
					dirs++
				}
			}
		}

		require.Equal(t, 3+3*4, dirs, "every directory's tree is among them")

		if located {
			require.NotZero(t, seen.fetches.Load(), "the trees come from the bucket")
		} else {
			require.Zero(t, seen.fetches.Load())
		}
	}
}
