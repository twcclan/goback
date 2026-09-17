package storage

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/twcclan/goback/auth"
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/presence"
	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	pb "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// NewRemoteClient connects to a store server over TLS, presenting creds
// on every call. A nil tlsConfig trusts the system roots; see ClientTLS
// for a pinned certificate authority.
func NewRemoteClient(addr string, creds auth.Credentials, tlsConfig *tls.Config) (*RemoteClient, error) {
	con, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		grpc.WithPerRPCCredentials(creds),
	)
	if err != nil {
		return nil, err
	}

	return &RemoteClient{
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

var (
	_ backup.Index      = (*RemoteClient)(nil)
	_ backup.Retention  = (*RemoteClient)(nil)
	_ backup.PartReader = (*RemoteClient)(nil)
	_ backup.CommitGate = (*RemoteClient)(nil)
)

// RemoteClient is a backup.Index over a store server's gRPC API.
type RemoteClient struct {
	store proto.StoreClient
}

// Open is a no-op; the connection is dialed by NewRemoteClient.
func (r *RemoteClient) Open() error  { return nil }
// Close is a no-op; close the connection instead.
func (r *RemoteClient) Close() error { return nil }

// FileInfo implements backup.Index.
func (r *RemoteClient) FileInfo(ctx context.Context, set string, name string, notAfter time.Time, count int) ([]*proto.TreeNode, error) {
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

// CommitInfo implements backup.Index.
func (r *RemoteClient) CommitInfo(ctx context.Context, set string, notAfter time.Time, count int) ([]*proto.Commit, error) {
	ctx = r.outgoing(ctx)

	na := timestamppb.New(notAfter)

	response, err := r.store.CommitInfo(ctx, &proto.CommitInfoRequest{
		BackupSet: set,
		NotAfter:  na,
		Count:     int32(count),
	})

	if err != nil {
		return nil, err
	}

	return response.Commits, nil
}

// ReIndex implements backup.Index.
func (r *RemoteClient) ReIndex(ctx context.Context) error {
	ctx = r.outgoing(ctx)

	return errors.New("not supported")
}

// LatestCommit implements backup.Index.
func (r *RemoteClient) LatestCommit(ctx context.Context, set string) (*proto.Ref, error) {
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
func (r *RemoteClient) GetTree(ctx context.Context, ref *proto.Ref, maxDepth uint32) ([]*proto.Object, error) {
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
			if status.Code(err) == codes.NotFound {
				return nil, backup.ErrNotFound
			}

			return nil, err
		}

		objects = append(objects, resp.Object)
	}
}

// Put implements backup.ObjectStore.
func (r *RemoteClient) Put(ctx context.Context, object *proto.Object) error {
	_, err := r.put(ctx, object, nil)

	return err
}

// PutFile implements backup.Confirmer.
func (r *RemoteClient) PutFile(ctx context.Context, object *proto.Object, assumed []*proto.Ref) ([]*proto.Ref, error) {
	return r.put(ctx, object, assumed)
}

func (r *RemoteClient) put(ctx context.Context, object *proto.Object, assumed []*proto.Ref) ([]*proto.Ref, error) {
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
		if strings.Contains(msg, backup.ErrSetOwned.Error()) {
			return nil, fmt.Errorf("%w: %s", backup.ErrSetOwned, msg)
		}

		if strings.Contains(msg, backup.ErrNoSession.Error()) {
			return nil, fmt.Errorf("%w: %s", backup.ErrNoSession, msg)
		}

		return nil, fmt.Errorf("%w: %s", backup.ErrDanglingRef, msg)
	}

	if err != nil {
		return nil, err
	}

	return resp.Missing, nil
}

// Presence implements backup.PresenceSource; a server without the RPC
// yields no filters.
func (r *RemoteClient) Presence(ctx context.Context, set string) (presence.Set, error) {
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

		if status.Code(err) == codes.Unimplemented {
			return nil, nil
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

// Get implements backup.ObjectStore.
func (r *RemoteClient) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	ctx = r.outgoing(ctx)

	resp, err := r.store.Get(ctx, &proto.GetRequest{Ref: ref})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, backup.ErrNotFound
		}

		return nil, err
	}

	if resp.Object == nil {
		return nil, backup.ErrNotFound
	}

	return resp.Object, nil
}

// Delete is not offered by the server; retention deletes commits and sets.
func (r *RemoteClient) Delete(context.Context, *proto.Ref) error { return backup.ErrNotImplemented }

// Walk is not offered by the server; an agent reads through Get and ReadParts.
func (r *RemoteClient) Walk(context.Context, bool, proto.ObjectType, backup.ObjectReceiver) error {
	return backup.ErrNotImplemented
}

// BeginCommit implements backup.CommitGate; a refusal is ErrCommitDenied
// with the server's reason.
func (r *RemoteClient) BeginCommit(ctx context.Context, set string) (*backup.CommitGrant, error) {
	resp, err := r.store.BeginCommit(r.outgoing(ctx), &proto.BeginCommitRequest{BackupSet: set})
	if err != nil {
		return nil, err
	}

	if !resp.Allowed {
		return nil, fmt.Errorf("%w: %s", backup.ErrCommitDenied, resp.Reason)
	}

	return &backup.CommitGrant{SetID: resp.SetId, Policy: policyFromProto(resp.Policy)}, nil
}

func policyFromProto(p *proto.StorePolicy) *storekey.Policy {
	if p == nil {
		return nil
	}

	return &storekey.Policy{
		Version: p.Version, Mode: storekey.Mode(p.Mode), SizeThreshold: p.SizeThreshold,
		EntropyEstimator: p.EntropyEstimator, EntropyThreshold: p.EntropyThreshold,
		PresenceScope: p.PresenceScope, Escrow: p.Escrow,
	}
}

func policyProto(p *storekey.Policy) *proto.StorePolicy {
	if p == nil {
		return nil
	}

	return &proto.StorePolicy{
		Version: p.Version, Mode: string(p.Mode), SizeThreshold: p.SizeThreshold,
		EntropyEstimator: p.EntropyEstimator, EntropyThreshold: p.EntropyThreshold,
		PresenceScope: p.PresenceScope, Escrow: p.Escrow,
	}
}

// ReadParts implements backup.PartReader through the ReadFile stream.
func (r *RemoteClient) ReadParts(ctx context.Context, file *proto.Ref, skip []int, fn func(int, *proto.Object) error) error {
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
			if status.Code(err) == codes.NotFound {
				return backup.ErrNotFound
			}

			return err
		}

		err = fn(int(resp.Index), resp.Object)
		if err != nil {
			return err
		}
	}
}

// DeleteCommit implements backup.Retention.
func (r *RemoteClient) DeleteCommit(ctx context.Context, ref *proto.Ref) error {
	_, err := r.store.DeleteCommit(r.outgoing(ctx), &proto.DeleteCommitRequest{Ref: ref})
	return err
}

// UndeleteCommit implements backup.Retention.
func (r *RemoteClient) UndeleteCommit(ctx context.Context, ref *proto.Ref) error {
	_, err := r.store.UndeleteCommit(r.outgoing(ctx), &proto.UndeleteCommitRequest{Ref: ref})
	return err
}

// DeleteSet implements backup.Retention.
func (r *RemoteClient) DeleteSet(ctx context.Context, set string, erase bool) error {
	_, err := r.store.DeleteSet(r.outgoing(ctx), &proto.DeleteSetRequest{BackupSet: set, Erase: erase})
	return err
}

// UndeleteSet implements backup.Retention.
func (r *RemoteClient) UndeleteSet(ctx context.Context, set string) error {
	_, err := r.store.UndeleteSet(r.outgoing(ctx), &proto.UndeleteSetRequest{BackupSet: set})
	return err
}

// Unpin implements backup.Retention.
func (r *RemoteClient) Unpin(ctx context.Context, pin *proto.Ref) error {
	_, err := r.store.Unpin(r.outgoing(ctx), &proto.UnpinRequest{Pin: pin})
	return err
}

// Pins implements backup.Retention.
func (r *RemoteClient) Pins(ctx context.Context) ([]*proto.PinInfo, error) {
	resp, err := r.store.ListPins(r.outgoing(ctx), &proto.ListPinsRequest{})
	if err != nil {
		return nil, err
	}

	return resp.GetPins(), nil
}

// Has is not offered by the server; presence filters answer it.
func (r *RemoteClient) Has(context.Context, *proto.Ref) (bool, error) {
	return false, backup.ErrNotImplemented
}

// NewRemoteServer serves store over gRPC.
func NewRemoteServer(store *Store) *RemoteServer {
	return &RemoteServer{store: store}
}

var _ proto.StoreServer = (*RemoteServer)(nil)

// RemoteServer is the gRPC adapter of a Store: every RPC is one Store
// operation and a translation of its errors to status codes.
type RemoteServer struct {
	proto.UnsafeStoreServer
	store *Store
}

// Store is the store the server serves.
func (r *RemoteServer) Store() *Store { return r.store }

// BeginCommit implements proto.StoreServer.
func (r *RemoteServer) BeginCommit(ctx context.Context, request *proto.BeginCommitRequest) (*proto.BeginCommitResponse, error) {
	grant, err := r.store.BeginCommit(ctx, request.BackupSet)
	if errors.Is(err, backup.ErrCommitDenied) {
		return &proto.BeginCommitResponse{Allowed: false, Reason: err.Error()}, nil
	}

	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.BeginCommitResponse{SetId: grant.SetID, Allowed: true, Policy: policyProto(grant.Policy)}, nil
}

// FileInfo implements proto.StoreServer.
func (r *RemoteServer) FileInfo(ctx context.Context, request *proto.FileInfoRequest) (*proto.FileInfoResponse, error) {
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

// CommitInfo implements proto.StoreServer.
func (r *RemoteServer) CommitInfo(ctx context.Context, request *proto.CommitInfoRequest) (*proto.CommitInfoResponse, error) {
	err := request.NotAfter.CheckValid()
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	commits, err := r.store.Index.CommitInfo(ctx, request.BackupSet, request.NotAfter.AsTime(), int(request.Count))
	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.CommitInfoResponse{Commits: commits}, nil
}

// Put implements proto.StoreServer.
func (r *RemoteServer) Put(ctx context.Context, request *proto.PutRequest) (*proto.PutResponse, error) {
	receipt, err := r.store.Put(ctx, Upload{Object: request.GetObject(), Ref: request.Ref, Assumed: request.AssumedRefs})
	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.PutResponse{Ref: receipt.Ref, Object: receipt.Object, Missing: receipt.Missing}, nil
}

// LatestCommit answers with an empty ref when the set has no commit.
func (r *RemoteServer) LatestCommit(ctx context.Context, request *proto.LatestCommitRequest) (*proto.LatestCommitResponse, error) {
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
func (r *RemoteServer) GetTree(request *proto.GetTreeRequest, stream proto.Store_GetTreeServer) error {
	return ToStatus(r.store.Tree(stream.Context(), request.Ref, request.MaxDepth, func(ref *proto.Ref, obj *proto.Object) error {
		return stream.Send(&proto.GetTreeResponse{Ref: ref, Object: obj})
	}))
}

// Get implements proto.StoreServer.
func (r *RemoteServer) Get(ctx context.Context, request *proto.GetRequest) (*proto.GetResponse, error) {
	obj, err := r.store.Get(ctx, request.Ref)
	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.GetResponse{Object: obj}, nil
}

// ReadFile streams the stored objects of a file's parts in order, running
// the fetch loop server-side so the client authorises once per file.
func (r *RemoteServer) ReadFile(request *proto.ReadFileRequest, stream proto.Store_ReadFileServer) error {
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
	case errors.Is(err, ErrInvalidRequest), errors.Is(err, proto.ErrRefMismatch), errors.Is(err, proto.ErrInvalidObject):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, backup.ErrDanglingRef), errors.Is(err, backup.ErrSetOwned), errors.Is(err, backup.ErrSetClosed),
		errors.Is(err, backup.ErrTombstoned), errors.Is(err, backup.ErrNewestCommit), errors.Is(err, backup.ErrOutOfOrder),
		errors.Is(err, backup.ErrPinned), errors.Is(err, backup.ErrNoSession), errors.Is(err, backup.ErrCommitDenied):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, backup.ErrNotImplemented):
		return status.Error(codes.Unimplemented, err.Error())
	}

	return err
}

// DeleteCommit implements proto.StoreServer.
func (r *RemoteServer) DeleteCommit(ctx context.Context, request *proto.DeleteCommitRequest) (*proto.DeleteCommitResponse, error) {
	ret, err := r.store.Retention()
	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.DeleteCommitResponse{}, ToStatus(ret.DeleteCommit(ctx, request.GetRef()))
}

// UndeleteCommit implements proto.StoreServer.
func (r *RemoteServer) UndeleteCommit(ctx context.Context, request *proto.UndeleteCommitRequest) (*proto.UndeleteCommitResponse, error) {
	ret, err := r.store.Retention()
	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.UndeleteCommitResponse{}, ToStatus(ret.UndeleteCommit(ctx, request.GetRef()))
}

// DeleteSet implements proto.StoreServer.
func (r *RemoteServer) DeleteSet(ctx context.Context, request *proto.DeleteSetRequest) (*proto.DeleteSetResponse, error) {
	ret, err := r.store.Retention()
	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.DeleteSetResponse{}, ToStatus(ret.DeleteSet(ctx, request.GetBackupSet(), request.GetErase()))
}

// UndeleteSet implements proto.StoreServer.
func (r *RemoteServer) UndeleteSet(ctx context.Context, request *proto.UndeleteSetRequest) (*proto.UndeleteSetResponse, error) {
	ret, err := r.store.Retention()
	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.UndeleteSetResponse{}, ToStatus(ret.UndeleteSet(ctx, request.GetBackupSet()))
}

// Unpin implements proto.StoreServer.
func (r *RemoteServer) Unpin(ctx context.Context, request *proto.UnpinRequest) (*proto.UnpinResponse, error) {
	ret, err := r.store.Retention()
	if err != nil {
		return nil, ToStatus(err)
	}

	return &proto.UnpinResponse{}, ToStatus(ret.Unpin(ctx, request.GetPin()))
}

// ListPins implements proto.StoreServer.
func (r *RemoteServer) ListPins(ctx context.Context, _ *proto.ListPinsRequest) (*proto.ListPinsResponse, error) {
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

// presencePiece is the largest data slice one GetPresence message carries.
const presencePiece = 1 << 20

// GetPresence streams the head filters of the caller's scope, each split
// into pieces that share an index.
func (r *RemoteServer) GetPresence(request *proto.GetPresenceRequest, stream proto.Store_GetPresenceServer) error {
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
