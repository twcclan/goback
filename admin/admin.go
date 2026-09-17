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
	"github.com/twcclan/goback/backup/retention"
	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/index"
	"github.com/twcclan/goback/index/sql"
	pb "github.com/twcclan/goback/proto/admin"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// m maps what the index reports to the admin protos.
var m = gen.MapperImpl{}

// Index is what the admin surface needs from a store's index.
type Index interface {
	ListSets(ctx context.Context) ([]index.SetInfo, error)
	TransferSet(ctx context.Context, name, agentID string) error
	DeleteSet(ctx context.Context, name string, erase bool) error
	UndeleteSet(ctx context.Context, name string) error
	GetStorePolicy(ctx context.Context) (index.StorePolicy, error)
	SetStorePolicy(ctx context.Context, policy storekey.Policy, acknowledge bool, now time.Time) (index.StorePolicy, error)
	GetPolicy(ctx context.Context, set string) (index.SetRetention, error)
	SetPolicy(ctx context.Context, set string, p *retention.Policy) error
	GetDefaultPolicy(ctx context.Context) (retention.Policy, bool, error)
	SetDefaultPolicy(ctx context.Context, p *retention.Policy) error
	Windows(ctx context.Context) (index.Windows, error)
	SetWindows(ctx context.Context, w index.Windows) error
}

// Server implements the Admin service over a store index.
type Server struct {
	pb.UnimplementedAdminServer

	Index Index
	// RetireJob and CollectJob run the retention and garbage collection
	// jobs; nil means the job is not available.
	RetireJob  func(ctx context.Context) (int, error)
	CollectJob func(ctx context.Context) (string, error)
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

var _ pb.AdminServer = (*Server)(nil)
var _ Index = (*sql.Index)(nil)

// Register implements Service.
func (s *Server) Register(srv *grpc.Server) { pb.RegisterAdminServer(srv, s) }

// RegisterGateway implements Service.
func (s *Server) RegisterGateway(ctx context.Context, mux *runtime.ServeMux) error {
	return pb.RegisterAdminHandlerServer(ctx, mux, s)
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}

	return time.Now()
}

// ListSets implements pb.AdminServer.
func (s *Server) ListSets(ctx context.Context, request *pb.ListSetsRequest) (*pb.ListSetsResponse, error) {
	sets, err := s.Index.ListSets(ctx)
	if err != nil {
		return nil, Status(err)
	}

	resp := &pb.ListSetsResponse{}
	for _, set := range sets {
		resp.Sets = append(resp.Sets, m.Set(set))
	}

	return resp, nil
}

// TransferSet implements pb.AdminServer.
func (s *Server) TransferSet(ctx context.Context, request *pb.TransferSetRequest) (*pb.BackupSet, error) {
	err := s.Index.TransferSet(ctx, request.Name, request.AgentId)
	if err != nil {
		return nil, Status(err)
	}

	return s.set(ctx, request.Name)
}

func (s *Server) set(ctx context.Context, name string) (*pb.BackupSet, error) {
	sets, err := s.Index.ListSets(ctx)
	if err != nil {
		return nil, Status(err)
	}

	for _, set := range sets {
		if set.Name == name {
			return m.Set(set), nil
		}
	}

	return nil, status.Errorf(codes.NotFound, "set %q not found", name)
}

// DeleteSet implements pb.AdminServer.
func (s *Server) DeleteSet(ctx context.Context, request *pb.DeleteSetRequest) (*pb.DeleteSetResponse, error) {
	err := s.Index.DeleteSet(ctx, request.Name, request.Erase)
	if err != nil {
		return nil, Status(err)
	}

	return &pb.DeleteSetResponse{}, nil
}

// UndeleteSet implements pb.AdminServer.
func (s *Server) UndeleteSet(ctx context.Context, request *pb.UndeleteSetRequest) (*pb.UndeleteSetResponse, error) {
	err := s.Index.UndeleteSet(ctx, request.Name)
	if err != nil {
		return nil, Status(err)
	}

	return &pb.UndeleteSetResponse{}, nil
}

// GetStorePolicy implements pb.AdminServer.
func (s *Server) GetStorePolicy(ctx context.Context, _ *pb.GetStorePolicyRequest) (*pb.StorePolicy, error) {
	p, err := s.Index.GetStorePolicy(ctx)
	if err != nil {
		return nil, Status(err)
	}

	return m.Policy(p), nil
}

// SetStorePolicy implements pb.AdminServer.
func (s *Server) SetStorePolicy(ctx context.Context, request *pb.SetStorePolicyRequest) (*pb.StorePolicy, error) {
	policy := storekey.Policy{
		Mode:             storekey.Mode(request.Mode),
		SizeThreshold:    request.SizeThreshold,
		EntropyEstimator: request.EntropyEstimator,
		EntropyThreshold: request.EntropyThreshold,
		PresenceScope:    request.PresenceScope,
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
		return nil, Status(err)
	}

	return m.Policy(p), nil
}

// GetRetention implements pb.AdminServer.
func (s *Server) GetRetention(ctx context.Context, request *pb.GetRetentionRequest) (*pb.Retention, error) {
	return s.retention(ctx, request.Set)
}

// SetRetention implements pb.AdminServer.
func (s *Server) SetRetention(ctx context.Context, request *pb.SetRetentionRequest) (*pb.Retention, error) {
	var policy *retention.Policy
	if request.Policy != nil {
		p := fromPolicy(request.Policy)
		policy = &p
	}

	switch {
	case request.Set != "" && (policy != nil || request.Inherit):
		if err := s.Index.SetPolicy(ctx, request.Set, policy); err != nil {
			return nil, Status(err)
		}
	case request.Set == "" && (policy != nil || request.Inherit):
		if err := s.Index.SetDefaultPolicy(ctx, policy); err != nil {
			return nil, Status(err)
		}
	}

	if request.Set == "" && (request.HoldDays != nil || request.TrashDays != nil) {
		w, err := s.Index.Windows(ctx)
		if err != nil {
			return nil, Status(err)
		}

		if request.HoldDays != nil {
			w.HoldDays = int(*request.HoldDays)
		}

		if request.TrashDays != nil {
			w.TrashDays = int(*request.TrashDays)
		}

		if err := s.Index.SetWindows(ctx, w); err != nil {
			return nil, Status(err)
		}
	}

	return s.retention(ctx, request.Set)
}

func (s *Server) retention(ctx context.Context, set string) (*pb.Retention, error) {
	w, err := s.Index.Windows(ctx)
	if err != nil {
		return nil, Status(err)
	}

	resp := &pb.Retention{Set: set, HoldDays: int32(w.HoldDays), TrashDays: int32(w.TrashDays)}

	if set == "" {
		policy, stored, err := s.Index.GetDefaultPolicy(ctx)
		if err != nil {
			return nil, Status(err)
		}

		resp.Effective = toPolicy(policy)
		if stored {
			resp.Policy = resp.Effective
		}

		return resp, nil
	}

	ret, err := s.Index.GetPolicy(ctx, set)
	if err != nil {
		return nil, Status(err)
	}

	resp.Effective = toPolicy(ret.Effective)
	resp.Paused = ret.Paused
	if ret.Policy != nil {
		resp.Policy = toPolicy(*ret.Policy)
	}

	return resp, nil
}

func toPolicy(p retention.Policy) *pb.RetentionPolicy {
	out := &pb.RetentionPolicy{
		KeepLast:    int32(p.KeepLast),
		KeepHourly:  int32(p.KeepHourly),
		KeepDaily:   int32(p.KeepDaily),
		KeepWeekly:  int32(p.KeepWeekly),
		KeepMonthly: int32(p.KeepMonthly),
	}
	if p.KeepWithin > 0 {
		out.KeepWithin = int64(p.KeepWithin / time.Second)
	}

	return out
}

func fromPolicy(p *pb.RetentionPolicy) retention.Policy {
	return retention.Policy{
		KeepLast:    int(p.KeepLast),
		KeepHourly:  int(p.KeepHourly),
		KeepDaily:   int(p.KeepDaily),
		KeepWeekly:  int(p.KeepWeekly),
		KeepMonthly: int(p.KeepMonthly),
		KeepWithin:  time.Duration(p.KeepWithin) * time.Second,
	}
}

// Retire implements pb.AdminServer.
func (s *Server) Retire(ctx context.Context, _ *pb.RetireRequest) (*pb.RetireResponse, error) {
	if s.RetireJob == nil {
		return nil, status.Error(codes.Unimplemented, "this index keeps no retention state")
	}

	n, err := s.RetireJob(ctx)
	if err != nil {
		return nil, Status(err)
	}

	return &pb.RetireResponse{Retired: int64(n)}, nil
}

// CollectGarbage implements pb.AdminServer.
func (s *Server) CollectGarbage(ctx context.Context, _ *pb.CollectGarbageRequest) (*pb.CollectGarbageResponse, error) {
	if s.CollectJob == nil {
		return nil, status.Error(codes.Unimplemented, "this store cannot garbage collect itself")
	}

	report, err := s.CollectJob(ctx)
	if err != nil {
		return nil, Status(err)
	}

	return &pb.CollectGarbageResponse{Report: report}, nil
}

// Status maps the index's sentinel errors to gRPC codes; an error that
// already is a status passes through.
func Status(err error) error {
	switch {
	case errors.Is(err, backup.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, backup.ErrTombstoned), errors.Is(err, backup.ErrSetClosed), errors.Is(err, backup.ErrSetOwned):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, auth.ErrForbidden):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, retention.ErrInvalidPolicy):
		return status.Error(codes.InvalidArgument, err.Error())
	}

	if _, ok := status.FromError(err); ok {
		return err
	}

	return status.Error(codes.Internal, err.Error())
}

const header = "authorization"

// Authorized reports whether the metadata carries the admin token.
func Authorized(md metadata.MD, token string) bool {
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
		if !Authorized(md, token) {
			return nil, status.Error(codes.Unauthenticated, "admin token required")
		}

		return handler(ctx, req)
	}
}

// Credentials presents the admin token on every call.
type Credentials string

// GetRequestMetadata implements credentials.PerRPCCredentials.
func (c Credentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{header: "Bearer " + string(c)}, nil
}

// RequireTransportSecurity implements credentials.PerRPCCredentials.
func (Credentials) RequireTransportSecurity() bool { return false }
