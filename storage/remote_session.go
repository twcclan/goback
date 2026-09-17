package storage

import (
	"context"
	"time"

	"github.com/twcclan/goback/auth"
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// sessionHeader carries the session id on every call of a session.
const sessionHeader = "goback-session"

var _ backup.SessionStore = (*RemoteClient)(nil)

// outgoing adds the session header when the context carries a session.
func (r *RemoteClient) outgoing(ctx context.Context) context.Context {
	s, ok := backup.SessionFromContext(ctx)
	if !ok {
		return ctx
	}

	return metadata.AppendToOutgoingContext(ctx, sessionHeader, s.ID)
}

// BeginSession opens a session on the server and returns a context whose
// calls belong to it.
func (r *RemoteClient) BeginSession(ctx context.Context, s *backup.Session) (context.Context, error) {
	resp, err := r.store.BeginSession(ctx, &proto.BeginSessionRequest{BackupSet: s.Set, Restore: s.Restore})
	if err != nil {
		return nil, err
	}

	s.ID = resp.SessionId
	s.Started = time.Now()

	return backup.WithSession(ctx, s), nil
}

// EndSession drops what the context's session has not committed.
func (r *RemoteClient) EndSession(ctx context.Context) error {
	s, ok := backup.SessionFromContext(ctx)
	if !ok {
		return nil
	}

	_, err := r.store.EndSession(ctx, &proto.EndSessionRequest{SessionId: s.ID})

	return err
}

func (r *RemoteServer) BeginSession(ctx context.Context, request *proto.BeginSessionRequest) (*proto.BeginSessionResponse, error) {
	session, err := r.store.BeginSession(ctx, request.BackupSet, request.Restore)
	if err != nil {
		return nil, toStatus(err)
	}

	return &proto.BeginSessionResponse{SessionId: session.ID, LeaseSeconds: int64(r.store.Lease / time.Second)}, nil
}

func (r *RemoteServer) EndSession(ctx context.Context, request *proto.EndSessionRequest) (*proto.EndSessionResponse, error) {
	err := r.store.EndSession(ctx, request.SessionId)
	if err != nil {
		return nil, toStatus(err)
	}

	return &proto.EndSessionResponse{}, nil
}

// withSession attaches the session named by the request metadata, if any.
func (r *RemoteServer) withSession(ctx context.Context) (context.Context, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	ids := md.Get(sessionHeader)
	if len(ids) == 0 {
		return ctx, nil
	}

	session, err := r.store.Session(ctx, ids[0])
	if err != nil {
		return nil, toStatus(err)
	}

	return backup.WithSession(ctx, session), nil
}

// UnaryInterceptor resolves the session header; it runs after
// authentication.
func (r *RemoteServer) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		ctx, err := r.admit(ctx)
		if err != nil {
			return nil, err
		}

		return handler(ctx, req)
	}
}

// StreamInterceptor is UnaryInterceptor for streaming calls.
func (r *RemoteServer) StreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, err := r.admit(ss.Context())
		if err != nil {
			return err
		}

		return handler(srv, &sessionStream{ServerStream: ss, ctx: ctx})
	}
}

func (r *RemoteServer) admit(ctx context.Context) (context.Context, error) {
	if _, err := auth.Require(ctx); err != nil {
		return nil, status.Error(codes.Unauthenticated, err.Error())
	}

	return r.withSession(ctx)
}

type sessionStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *sessionStream) Context() context.Context { return s.ctx }
