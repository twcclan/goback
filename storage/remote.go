package storage

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/twcclan/goback/auth"
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/presence"
	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/storage/pack"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	pb "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// NewClient connects to a store server over TLS, presenting creds
// on every call. A nil tlsConfig trusts the system roots; see ClientTLS
// for a pinned certificate authority.
func NewClient(addr string, creds auth.Credentials, tlsConfig *tls.Config) (*Client, error) {
	return dialStore(addr, creds, credentials.NewTLS(tlsConfig), true)
}

// NewPlaintextClient connects to a store server without TLS, putting the
// credentials and every object on the wire in the clear. It is for a
// store on the same machine as the agent; anywhere else, use NewClient.
// The credentials must allow it.
func NewPlaintextClient(addr string, creds auth.Credentials) (*Client, error) {
	if !creds.Plaintext {
		return nil, errors.New("plaintext credentials are required to dial a store without TLS")
	}

	return dialStore(addr, creds, insecure.NewCredentials(), false)
}

func dialStore(addr string, creds auth.Credentials, transport credentials.TransportCredentials, secure bool) (*Client, error) {
	jar := newCookies(addr, secure)
	con, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(transport),
		grpc.WithPerRPCCredentials(creds),
		grpc.WithUnaryInterceptor(jar.unary()),
		grpc.WithStreamInterceptor(jar.stream()),
	)
	if err != nil {
		return nil, err
	}

	return &Client{
		store: proto.NewStoreClient(con),
	}, nil
}

// ClientTLS returns a client TLS configuration that trusts only the PEM
// certificates in caFile, or the system roots when caFile is empty.
func ClientTLS(caFile string) (*tls.Config, error) {
	if caFile == "" {
		return nil, nil
	}

	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, err
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("no certificates in %s", caFile)
	}

	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, nil
}

// ServerTLS returns what a store server listens with: TLS from the
// certificate and key files, or plaintext when the operator declared a
// TLS-terminating proxy in front and gave neither. The TLS configuration
// is nil for plaintext.
func ServerTLS(certFile, keyFile string, plaintextBehindProxy bool) (credentials.TransportCredentials, *tls.Config, error) {
	switch {
	case certFile != "" && keyFile != "":
		if plaintextBehindProxy {
			return nil, nil, errors.New("--plaintext-behind-proxy and --tls-cert are exclusive")
		}

		pair, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, nil, err
		}

		config := &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}

		return credentials.NewTLS(config), config, nil
	case certFile != "" || keyFile != "":
		return nil, nil, errors.New("--tls-cert and --tls-key go together")
	case plaintextBehindProxy:
		return insecure.NewCredentials(), nil, nil
	default:
		return nil, nil, errors.New("--tls-cert and --tls-key are required unless --plaintext-behind-proxy is set")
	}
}

var (
	_ backup.Index      = (*Client)(nil)
	_ backup.Retention  = (*Client)(nil)
	_ backup.PartReader = (*Client)(nil)
	_ backup.DirLister  = (*Client)(nil)
	_ backup.CommitGate = (*Client)(nil)
)

// Client is a backup.Index over a store server's gRPC API.
type Client struct {
	store proto.StoreClient

	// HTTP fetches the locations a server answers with; nil uses the
	// default client.
	HTTP *http.Client
}

// Open is a no-op; the connection is dialed by NewClient.
func (r *Client) Open() error { return nil }

// Close is a no-op; close the connection instead.
func (r *Client) Close() error { return nil }

// FileInfo implements backup.Index.
func (r *Client) FileInfo(ctx context.Context, set string, name string, notAfter time.Time, count int) ([]*proto.TreeNode, error) {
	ctx = r.outgoing(ctx)

	na := timestamppb.New(notAfter)

	response, err := r.store.FileInfo(ctx, &proto.FileInfoRequest{
		BackupSet: set,
		Path:      name,
		NotAfter:  na,
		Count:     int32(count),
	})

	if err != nil {
		return nil, err
	}

	return response.Files, nil
}

// ReadDir implements backup.DirLister.
func (r *Client) ReadDir(ctx context.Context, set string, dir string, notAfter time.Time) ([]*proto.TreeNode, error) {
	ctx = r.outgoing(ctx)

	response, err := r.store.ReadDir(ctx, &proto.ReadDirRequest{
		BackupSet: set,
		Path:      dir,
		NotAfter:  timestamppb.New(notAfter),
	})

	if err != nil {
		return nil, err
	}

	return response.Entries, nil
}

// CommitInfo implements backup.Index.
func (r *Client) CommitInfo(ctx context.Context, set string, notAfter time.Time, count int) ([]*proto.Commit, error) {
	commits, _, err := r.CommitSizes(ctx, set, notAfter, count)

	return commits, err
}

// CommitSizes implements backup.CommitSizer. A store that measures nothing
// answers each commit with an empty size.
func (r *Client) CommitSizes(ctx context.Context, set string, notAfter time.Time, count int) ([]*proto.Commit, []*proto.CommitSize, error) {
	ctx = r.outgoing(ctx)

	response, err := r.store.CommitInfo(ctx, &proto.CommitInfoRequest{
		BackupSet: set,
		NotAfter:  timestamppb.New(notAfter),
		Count:     int32(count),
	})
	if err != nil {
		return nil, nil, err
	}

	sizes := response.Sizes
	if len(sizes) != len(response.Commits) {
		sizes = make([]*proto.CommitSize, len(response.Commits))
		for i := range sizes {
			sizes[i] = &proto.CommitSize{}
		}
	}

	return response.Commits, sizes, nil
}

// ReIndex is not offered by the server.
func (r *Client) ReIndex(context.Context) error { return backup.ErrNotImplemented }

// LatestCommit implements backup.Index.
func (r *Client) LatestCommit(ctx context.Context, set string) (*proto.Ref, error) {
	ctx = r.outgoing(ctx)

	response, err := r.store.LatestCommit(ctx, &proto.LatestCommitRequest{BackupSet: set})
	if err != nil {
		return nil, err
	}

	if response.Ref == nil {
		return nil, backup.ErrNotFound
	}

	return response.Ref, nil
}

// GetTree implements backup.TreeFetcher over the streaming RPC.
func (r *Client) GetTree(ctx context.Context, ref *proto.Ref, maxDepth uint32) ([]*proto.Object, error) {
	ctx = r.outgoing(ctx)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := r.store.GetTree(ctx, &proto.GetTreeRequest{Ref: ref, MaxDepth: maxDepth})
	if err != nil {
		return nil, err
	}

	var objects []*proto.Object
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			return objects, nil
		}

		if err != nil {
			return nil, fromStatus(err)
		}

		objects = append(objects, resp.Object)
	}
}

// Put implements backup.ObjectStore.
func (r *Client) Put(ctx context.Context, object *proto.Object) error {
	_, err := r.put(ctx, object, nil)

	return err
}

// PutFile implements backup.Confirmer.
func (r *Client) PutFile(ctx context.Context, object *proto.Object, assumed []*proto.Ref) ([]*proto.Ref, error) {
	return r.put(ctx, object, assumed)
}

func (r *Client) put(ctx context.Context, object *proto.Object, assumed []*proto.Ref) ([]*proto.Ref, error) {
	ctx = r.outgoing(ctx)

	err := object.Validate()
	if err != nil {
		return nil, err
	}

	request := &proto.PutRequest{Object: object, AssumedRefs: assumed}
	stamped := object.GetCommit() != nil || object.GetPin() != nil
	if !stamped {
		request.Ref = object.Ref()
	}

	resp, err := r.store.Put(ctx, request)
	if err == nil && stamped && resp.GetObject() != nil {
		// the server assigned the receipt time and set id; the caller's
		// object must hash to the stored ref
		pb.Reset(object)
		pb.Merge(object, resp.Object)
	}

	if status.Code(err) == codes.FailedPrecondition {
		msg := status.Convert(err).Message()
		if strings.Contains(msg, backup.ErrNoSession.Error()) {
			return nil, fmt.Errorf("%w: %s", backup.ErrNoSession, msg)
		}

		return nil, fmt.Errorf("%w: %s", backup.ErrDanglingRef, msg)
	}

	if status.Code(err) == codes.Aborted {
		return nil, fmt.Errorf("%w: %s", backup.ErrSessionLost, status.Convert(err).Message())
	}

	if err != nil {
		return nil, err
	}

	return resp.Missing, nil
}

// Presence implements backup.PresenceSource.
func (r *Client) Presence(ctx context.Context, set string) (presence.Set, error) {
	ctx = r.outgoing(ctx)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := r.store.GetPresence(ctx, &proto.GetPresenceRequest{BackupSet: set})
	if err != nil {
		return nil, err
	}

	var (
		order   []uint32
		filters = map[uint32]*proto.PresenceFilter{}
	)

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, err
		}

		if f, ok := filters[resp.Index]; ok {
			f.Data = append(f.Data, resp.GetFilter().GetData()...)
			continue
		}

		filters[resp.Index] = resp.GetFilter()
		order = append(order, resp.Index)
	}

	result := make(presence.Set, 0, len(order))
	for _, index := range order {
		f, err := presence.FromProto(filters[index])
		if err != nil {
			return nil, err
		}

		result = append(result, f)
	}

	return result, nil
}

// Get implements backup.ObjectStore. A server that answers with a
// location is followed, so where the bytes came from stays its business.
func (r *Client) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	resp, err := r.store.Get(r.outgoing(ctx), &proto.GetRequest{Ref: ref})
	if err != nil {
		return nil, fromStatus(err)
	}

	if location := resp.GetLocation(); location != nil {
		return r.fetch(ctx, ref, location)
	}

	if resp.GetObject() == nil {
		return nil, backup.ErrNotFound
	}

	return resp.GetObject(), nil
}

// fetch follows a location and decodes the record it yields, refusing an
// object that is not the one asked for.
func (r *Client) fetch(ctx context.Context, ref *proto.Ref, location *proto.Location) (*proto.Object, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location.GetUrl(), nil)
	if err != nil {
		return nil, err
	}

	for name, value := range location.GetHeader() {
		request.Header.Set(name, value)
	}

	client := r.HTTP
	if client == nil {
		client = http.DefaultClient
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("fetching object %x from its location: %s", ref.GetHash(), response.Status)
	}

	record, err := io.ReadAll(io.LimitReader(response.Body, location.GetLength()))
	if err != nil {
		return nil, err
	}

	object, err := pack.DecodeRecord(record, location.GetAtRestKey())
	if err != nil {
		return nil, fmt.Errorf("decoding object %x from its location: %w", ref.GetHash(), err)
	}

	if !object.Ref().Equal(ref) {
		return nil, fmt.Errorf("%w: location for %x yielded %x", proto.ErrRefMismatch, ref.GetHash(), object.Ref().GetHash())
	}

	return object, nil
}

// EscrowedKeys returns every escrowed copy of the store key the server
// keeps.
func (r *Client) EscrowedKeys(ctx context.Context) ([]backup.EscrowedKey, error) {
	response, err := r.store.EscrowedKeys(r.outgoing(ctx), &proto.EscrowedKeysRequest{})
	if err != nil {
		return nil, err
	}

	kept := make([]backup.EscrowedKey, 0, len(response.GetKeys()))
	for _, k := range response.GetKeys() {
		kept = append(kept, backup.EscrowedKey{KeyID: k.GetKeyId(), Escrowed: k.GetEscrowed()})
	}

	return kept, nil
}

// Delete is not offered by the server; retention deletes commits and sets.
func (r *Client) Delete(context.Context, *proto.Ref) error { return backup.ErrNotImplemented }

// Walk is not offered by the server; an agent reads through Get and ReadParts.
func (r *Client) Walk(context.Context, bool, proto.ObjectType, backup.ObjectReceiver) error {
	return backup.ErrNotImplemented
}

// BeginCommit implements backup.CommitGate; a refusal is ErrCommitDenied
// with the server's reason.
func (r *Client) BeginCommit(ctx context.Context, set string) (*backup.CommitGrant, error) {
	resp, err := r.store.BeginCommit(r.outgoing(ctx), &proto.BeginCommitRequest{BackupSet: set})
	if err != nil {
		return nil, err
	}

	if !resp.Allowed {
		return nil, fmt.Errorf("%w: %s", backup.ErrCommitDenied, resp.Reason)
	}

	return &backup.CommitGrant{
		Policy:  policyFromProto(resp.Policy),
		Rescan:  resp.Rescan,
		Damaged: resp.DamagedPaths,
	}, nil
}

func policyFromProto(p *proto.StorePolicy) *storekey.Policy {
	if p == nil {
		return nil
	}

	return &storekey.Policy{Version: p.Version, Mode: storekey.Mode(p.Mode), PresenceScope: p.PresenceScope}
}

func policyProto(p *storekey.Policy) *proto.StorePolicy {
	if p == nil {
		return nil
	}

	return &proto.StorePolicy{Version: p.Version, Mode: string(p.Mode), PresenceScope: p.PresenceScope}
}

// ReadParts implements backup.PartReader through the ReadFile stream.
func (r *Client) ReadParts(ctx context.Context, file *proto.Ref, skip []int, fn func(int, *proto.Object) error) error {
	ctx, cancel := context.WithCancel(r.outgoing(ctx))
	defer cancel()

	request := &proto.ReadFileRequest{Ref: file, SkipParts: make([]uint32, 0, len(skip))}
	for _, i := range skip {
		request.SkipParts = append(request.SkipParts, uint32(i))
	}

	stream, err := r.store.ReadFile(ctx, request)
	if err != nil {
		return err
	}

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			return nil
		}

		if err != nil {
			return fromStatus(err)
		}

		err = fn(int(resp.Index), resp.Object)
		if err != nil {
			return err
		}
	}
}

// DeleteCommit implements backup.Retention.
func (r *Client) DeleteCommit(ctx context.Context, ref *proto.Ref) error {
	_, err := r.store.DeleteCommit(r.outgoing(ctx), &proto.DeleteCommitRequest{Ref: ref})
	return err
}

// UndeleteCommit implements backup.Retention.
func (r *Client) UndeleteCommit(ctx context.Context, ref *proto.Ref) error {
	_, err := r.store.UndeleteCommit(r.outgoing(ctx), &proto.UndeleteCommitRequest{Ref: ref})
	return err
}

// DeleteSet implements backup.Retention.
func (r *Client) DeleteSet(ctx context.Context, set string, erase bool) error {
	_, err := r.store.DeleteSet(r.outgoing(ctx), &proto.DeleteSetRequest{BackupSet: set, Erase: erase})
	return err
}

// UndeleteSet implements backup.Retention.
func (r *Client) UndeleteSet(ctx context.Context, set string) error {
	_, err := r.store.UndeleteSet(r.outgoing(ctx), &proto.UndeleteSetRequest{BackupSet: set})
	return err
}

// Unpin implements backup.Retention.
func (r *Client) Unpin(ctx context.Context, pin *proto.Ref) error {
	_, err := r.store.Unpin(r.outgoing(ctx), &proto.UnpinRequest{Pin: pin})
	return err
}

// Pins implements backup.Retention.
func (r *Client) Pins(ctx context.Context) ([]*proto.PinInfo, error) {
	resp, err := r.store.ListPins(r.outgoing(ctx), &proto.ListPinsRequest{})
	if err != nil {
		return nil, err
	}

	return resp.GetPins(), nil
}

// Has is not offered by the server; presence filters answer it.
func (r *Client) Has(context.Context, *proto.Ref) (bool, error) {
	return false, backup.ErrNotImplemented
}

// NewServer serves store over gRPC.
func NewServer(store *Store) *Server {
	return &Server{store: store}
}

var _ proto.StoreServer = (*Server)(nil)

// Server is the gRPC adapter of a Store: every RPC is one Store
// operation and a translation of its errors to status codes.
type Server struct {
	proto.UnsafeStoreServer
	store *Store
}

// BeginCommit implements proto.StoreServer.
func (r *Server) BeginCommit(ctx context.Context, request *proto.BeginCommitRequest) (*proto.BeginCommitResponse, error) {
	grant, err := r.store.BeginCommit(ctx, request.BackupSet)
	if errors.Is(err, backup.ErrCommitDenied) {
		return &proto.BeginCommitResponse{Allowed: false, Reason: err.Error()}, nil
	}

	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.BeginCommitResponse{
		Allowed:      true,
		Policy:       policyProto(grant.Policy),
		Rescan:       grant.Rescan,
		DamagedPaths: grant.Damaged,
	}, nil
}

// FileInfo implements proto.StoreServer.
func (r *Server) FileInfo(ctx context.Context, request *proto.FileInfoRequest) (*proto.FileInfoResponse, error) {
	err := request.NotAfter.CheckValid()
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	files, err := r.store.Index.FileInfo(ctx, request.BackupSet, request.Path, request.NotAfter.AsTime(), int(request.Count))
	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.FileInfoResponse{Files: files}, nil
}

// ReadDir implements proto.StoreServer.
func (r *Server) ReadDir(ctx context.Context, request *proto.ReadDirRequest) (*proto.ReadDirResponse, error) {
	err := request.NotAfter.CheckValid()
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	entries, err := r.store.ReadDir(ctx, request.GetBackupSet(), request.GetPath(), request.NotAfter.AsTime())
	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.ReadDirResponse{Entries: entries}, nil
}

// CommitInfo implements proto.StoreServer.
func (r *Server) CommitInfo(ctx context.Context, request *proto.CommitInfoRequest) (*proto.CommitInfoResponse, error) {
	err := request.NotAfter.CheckValid()
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if sizer, ok := r.store.Index.(backup.CommitSizer); ok {
		commits, sizes, err := sizer.CommitSizes(ctx, request.BackupSet, request.NotAfter.AsTime(), int(request.Count))
		if err != nil {
			return nil, ToStatus(err)
		}

		return &proto.CommitInfoResponse{Commits: commits, Sizes: sizes}, nil
	}

	commits, err := r.store.Index.CommitInfo(ctx, request.BackupSet, request.NotAfter.AsTime(), int(request.Count))
	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.CommitInfoResponse{Commits: commits}, nil
}

// Put implements proto.StoreServer.
func (r *Server) Put(ctx context.Context, request *proto.PutRequest) (*proto.PutResponse, error) {
	receipt, err := r.store.Put(ctx, Upload{Object: request.GetObject(), Ref: request.Ref, Assumed: request.AssumedRefs})
	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.PutResponse{Ref: receipt.Ref, Object: receipt.Object, Missing: receipt.Missing}, nil
}

// LatestCommit answers with an empty ref when the set has no commit.
func (r *Server) LatestCommit(ctx context.Context, request *proto.LatestCommitRequest) (*proto.LatestCommitResponse, error) {
	ref, err := r.store.Index.LatestCommit(ctx, request.BackupSet)
	if errors.Is(err, backup.ErrNotFound) {
		return &proto.LatestCommitResponse{}, nil
	}

	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.LatestCommitResponse{Ref: ref}, nil
}

// GetTree implements proto.StoreServer.
func (r *Server) GetTree(request *proto.GetTreeRequest, stream proto.Store_GetTreeServer) error {
	return ToStatus(r.store.Tree(stream.Context(), request.Ref, request.MaxDepth, func(ref *proto.Ref, obj *proto.Object) error {
		return stream.Send(&proto.GetTreeResponse{Ref: ref, Object: obj})
	}))
}

// Get implements proto.StoreServer.
func (r *Server) Get(ctx context.Context, request *proto.GetRequest) (*proto.GetResponse, error) {
	object, location, err := r.store.Read(ctx, request.Ref)
	if err != nil {
		return nil, ToStatus(err)
	}

	if location != nil {
		return &proto.GetResponse{Body: &proto.GetResponse_Location{Location: location}}, nil
	}

	return &proto.GetResponse{Body: &proto.GetResponse_Object{Object: object}}, nil
}

// ReadFile streams the stored objects of a file's parts in order, running
// the fetch loop server-side so the client authorises once per file.
func (r *Server) ReadFile(request *proto.ReadFileRequest, stream proto.Store_ReadFileServer) error {
	return ToStatus(r.store.ReadFile(stream.Context(), request.Ref, request.SkipParts, func(index int, obj *proto.Object) error {
		return stream.Send(&proto.ReadFileResponse{Index: uint32(index), Object: obj})
	}))
}

// ToStatus maps the store's sentinel errors to gRPC codes; an error that
// already is a status passes through.
func ToStatus(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, backup.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, auth.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, auth.ErrForbidden):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, ErrInvalidRequest), errors.Is(err, proto.ErrRefMismatch), errors.Is(err, proto.ErrInvalidObject),
		errors.Is(err, backup.ErrInvalidEscrow), errors.Is(err, backup.ErrSetName):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, backup.ErrDanglingRef), errors.Is(err, backup.ErrSetClosed),
		errors.Is(err, backup.ErrTombstoned), errors.Is(err, backup.ErrNewestCommit),
		errors.Is(err, backup.ErrPinned), errors.Is(err, backup.ErrNoSession), errors.Is(err, backup.ErrCommitDenied),
		errors.Is(err, backup.ErrOtherKeyEscrowed):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, backup.ErrNotImplemented):
		return status.Error(codes.Unimplemented, err.Error())
	case errors.Is(err, backup.ErrSessionLost):
		return status.Error(codes.Aborted, err.Error())
	case errors.Is(err, backup.ErrQuotaExceeded):
		return status.Error(codes.ResourceExhausted, err.Error())
	}

	return err
}

// fromStatus maps the codes a read answers with back to the store's
// sentinel errors, keeping the server's message.
func fromStatus(err error) error {
	switch status.Code(err) {
	case codes.NotFound:
		return backup.ErrNotFound
	case codes.ResourceExhausted:
		return fmt.Errorf("%w: %s", backup.ErrQuotaExceeded, status.Convert(err).Message())
	}

	return err
}

// DeleteCommit implements proto.StoreServer.
func (r *Server) DeleteCommit(ctx context.Context, request *proto.DeleteCommitRequest) (*proto.DeleteCommitResponse, error) {
	ret, err := r.store.Retention()
	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.DeleteCommitResponse{}, ToStatus(ret.DeleteCommit(ctx, request.GetRef()))
}

// UndeleteCommit implements proto.StoreServer.
func (r *Server) UndeleteCommit(ctx context.Context, request *proto.UndeleteCommitRequest) (*proto.UndeleteCommitResponse, error) {
	ret, err := r.store.Retention()
	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.UndeleteCommitResponse{}, ToStatus(ret.UndeleteCommit(ctx, request.GetRef()))
}

// DeleteSet implements proto.StoreServer.
func (r *Server) DeleteSet(ctx context.Context, request *proto.DeleteSetRequest) (*proto.DeleteSetResponse, error) {
	ret, err := r.store.Retention()
	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.DeleteSetResponse{}, ToStatus(ret.DeleteSet(ctx, request.GetBackupSet(), request.GetErase()))
}

// UndeleteSet implements proto.StoreServer.
func (r *Server) UndeleteSet(ctx context.Context, request *proto.UndeleteSetRequest) (*proto.UndeleteSetResponse, error) {
	ret, err := r.store.Retention()
	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.UndeleteSetResponse{}, ToStatus(ret.UndeleteSet(ctx, request.GetBackupSet()))
}

// Unpin implements proto.StoreServer.
func (r *Server) Unpin(ctx context.Context, request *proto.UnpinRequest) (*proto.UnpinResponse, error) {
	ret, err := r.store.Retention()
	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.UnpinResponse{}, ToStatus(ret.Unpin(ctx, request.GetPin()))
}

// ListPins implements proto.StoreServer.
func (r *Server) ListPins(ctx context.Context, _ *proto.ListPinsRequest) (*proto.ListPinsResponse, error) {
	ret, err := r.store.Retention()
	if err != nil {
		return nil, ToStatus(err)
	}

	pins, err := ret.Pins(ctx)
	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.ListPinsResponse{Pins: pins}, nil
}

// EscrowedKeys implements proto.StoreServer.
func (r *Server) EscrowedKeys(ctx context.Context, _ *proto.EscrowedKeysRequest) (*proto.EscrowedKeysResponse, error) {
	escrow, err := r.store.Escrow()
	if err != nil {
		return nil, ToStatus(err)
	}

	kept, err := escrow.EscrowedKeys(ctx)
	if err != nil {
		return nil, ToStatus(err)
	}

	response := &proto.EscrowedKeysResponse{}
	for _, k := range kept {
		response.Keys = append(response.Keys, &proto.EscrowedKey{KeyId: k.KeyID, Escrowed: k.Escrowed})
	}

	return response, nil
}

// presencePiece is the largest data slice one GetPresence message carries.
const presencePiece = 1 << 20

// GetPresence streams the head filters of the caller's scope, each split
// into pieces that share an index.
func (r *Server) GetPresence(request *proto.GetPresenceRequest, stream proto.Store_GetPresenceServer) error {
	filters, err := r.store.Presence(stream.Context(), request.BackupSet)
	if err != nil {
		return ToStatus(err)
	}

	for i, filter := range filters {
		data := filter.Data
		header := pb.Clone(filter).(*proto.PresenceFilter)

		for first := true; first || len(data) > 0; first = false {
			n := len(data)
			if n > presencePiece {
				n = presencePiece
			}

			piece := &proto.PresenceFilter{}
			if first {
				piece = header
			}

			piece.Data = data[:n]

			err = stream.Send(&proto.GetPresenceResponse{Index: uint32(i), Filter: piece})
			if err != nil {
				return err
			}

			data = data[n:]
		}
	}

	return nil
}
