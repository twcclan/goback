// Package admin serves the operator surface of a store server: sets, the
// store policy and job triggers, over gRPC and REST behind one bearer
// token.
package admin

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"
	"time"

	"github.com/twcclan/goback/admin/mapping/gen"
	"github.com/twcclan/goback/auth"
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/index/sql"
	pb "github.com/twcclan/goback/proto/admin"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// m maps what the index reports to the admin protos.
var m = gen.MapperImpl{}

// Server implements the Admin service over a store index.
type Server struct {
	pb.UnimplementedAdminServer

	Index *sql.Index
	// RetireJob and CollectJob run the retention and garbage collection
	// jobs; nil means the job is not available.
	RetireJob  func(ctx context.Context) (int, error)
	CollectJob func(ctx context.Context) (string, error)
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

var _ pb.AdminServer = (*Server)(nil)

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}

	return time.Now()
}

func (s *Server) ListSets(ctx context.Context, request *pb.ListSetsRequest) (*pb.ListSetsResponse, error) {
	sets, err := s.Index.ListSets(ctx)
	if err != nil {
		return nil, toStatus(err)
	}

	resp := &pb.ListSetsResponse{}
	for _, set := range sets {
		resp.Sets = append(resp.Sets, m.Set(set))
	}

	return resp, nil
}

func (s *Server) TransferSet(ctx context.Context, request *pb.TransferSetRequest) (*pb.BackupSet, error) {
	err := s.Index.TransferSet(ctx, request.Name, request.AgentId)
	if err != nil {
		return nil, toStatus(err)
	}

	return s.set(ctx, request.Name)
}

func (s *Server) set(ctx context.Context, name string) (*pb.BackupSet, error) {
	sets, err := s.Index.ListSets(ctx)
	if err != nil {
		return nil, toStatus(err)
	}

	for _, set := range sets {
		if set.Name == name {
			return m.Set(set), nil
		}
	}

	return nil, status.Errorf(codes.NotFound, "set %q not found", name)
}

func (s *Server) DeleteSet(ctx context.Context, request *pb.DeleteSetRequest) (*pb.DeleteSetResponse, error) {
	err := s.Index.DeleteSet(ctx, request.Name, request.Erase)
	if err != nil {
		return nil, toStatus(err)
	}

	return &pb.DeleteSetResponse{}, nil
}

func (s *Server) UndeleteSet(ctx context.Context, request *pb.UndeleteSetRequest) (*pb.UndeleteSetResponse, error) {
	err := s.Index.UndeleteSet(ctx, request.Name)
	if err != nil {
		return nil, toStatus(err)
	}

	return &pb.UndeleteSetResponse{}, nil
}

func (s *Server) GetStorePolicy(ctx context.Context, _ *pb.GetStorePolicyRequest) (*pb.StorePolicy, error) {
	p, err := s.Index.GetStorePolicy(ctx)
	if err != nil {
		return nil, toStatus(err)
	}

	return m.Policy(p), nil
}

func (s *Server) SetStorePolicy(ctx context.Context, request *pb.SetStorePolicyRequest) (*pb.StorePolicy, error) {
	policy := storekey.Policy{
		Mode:             storekey.Mode(request.Mode),
		SizeThreshold:    request.SizeThreshold,
		EntropyEstimator: request.EntropyEstimator,
		EntropyThreshold: request.EntropyThreshold,
		PresenceScope:    request.PresenceScope,
		Escrow:           request.Escrow,
	}

	switch policy.Mode {
	case storekey.ModeHybrid, storekey.ModeConvergentAll, storekey.ModeStoreKeyedAll, storekey.ModeNone:
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unknown policy mode %q", request.Mode)
	}

	if _, err := backup.ParsePresenceScope(policy.PresenceScope); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	p, err := s.Index.SetStorePolicy(ctx, policy, request.AcknowledgeKey, s.now())
	if err != nil {
		return nil, toStatus(err)
	}

	return m.Policy(p), nil
}

func (s *Server) Retire(ctx context.Context, _ *pb.RetireRequest) (*pb.RetireResponse, error) {
	if s.RetireJob == nil {
		return nil, status.Error(codes.Unimplemented, "this index keeps no retention state")
	}

	n, err := s.RetireJob(ctx)
	if err != nil {
		return nil, toStatus(err)
	}

	return &pb.RetireResponse{Retired: int64(n)}, nil
}

func (s *Server) CollectGarbage(ctx context.Context, _ *pb.CollectGarbageRequest) (*pb.CollectGarbageResponse, error) {
	if s.CollectJob == nil {
		return nil, status.Error(codes.Unimplemented, "this store cannot garbage collect itself")
	}

	report, err := s.CollectJob(ctx)
	if err != nil {
		return nil, toStatus(err)
	}

	return &pb.CollectGarbageResponse{Report: report}, nil
}

// toStatus maps the index's sentinel errors to gRPC codes.
func toStatus(err error) error {
	switch {
	case errors.Is(err, backup.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, backup.ErrTombstoned), errors.Is(err, backup.ErrSetClosed), errors.Is(err, backup.ErrSetOwned):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, auth.ErrForbidden):
		return status.Error(codes.PermissionDenied, err.Error())
	}

	if _, ok := status.FromError(err); ok {
		return err
	}

	return status.Error(codes.Internal, err.Error())
}

const header = "authorization"

// authorized reports whether the metadata carries the admin token.
func authorized(md metadata.MD, token string) bool {
	for _, v := range md.Get(header) {
		presented := strings.TrimPrefix(v, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) == 1 {
			return true
		}
	}

	return false
}

// UnaryInterceptor rejects calls that do not present the admin token.
func UnaryInterceptor(token string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		if !authorized(md, token) {
			return nil, status.Error(codes.Unauthenticated, "admin token required")
		}

		return handler(ctx, req)
	}
}

// Credentials presents the admin token on every call.
type Credentials string

func (c Credentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{header: "Bearer " + string(c)}, nil
}

func (Credentials) RequireTransportSecurity() bool { return false }
