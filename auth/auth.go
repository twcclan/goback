// Package auth carries the caller's identity through a request: the one
// secret every agent of a server shares, and the agent id each agent
// declares.
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"

	"github.com/twcclan/goback/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Principal is who is calling: the agent id the caller declared.
type Principal struct {
	AgentID string
}

// ErrUnauthenticated is returned when a request carries no principal.
var ErrUnauthenticated = errors.New("no caller identity")

// ErrForbidden is returned when the principal may not do what it asked.
var ErrForbidden = errors.New("not allowed for this caller")

type principalKey struct{}

// WithPrincipal attaches the principal to the context.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext returns the request's principal, if any.
func FromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(*Principal)
	return p, ok && p != nil
}

// Require returns the principal or ErrUnauthenticated.
func Require(ctx context.Context) (*Principal, error) {
	p, ok := FromContext(ctx)
	if !ok {
		return nil, ErrUnauthenticated
	}

	return p, nil
}

// AuthorizeCommit checks that a commit names the principal's agent.
func (p *Principal) AuthorizeCommit(c *proto.Commit) error {
	if p.AgentID != "" && c.GetAgentId() != p.AgentID {
		return fmt.Errorf("%w: commit is by agent %q", ErrForbidden, c.GetAgentId())
	}

	return nil
}

const (
	secretHeader = "authorization"
	agentHeader  = "goback-agent"
)

// Credentials sends the shared secret and the agent id with every call and
// refuses to do so over a plaintext connection.
type Credentials struct {
	Secret  string
	AgentID string
}

func (c Credentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{secretHeader: "Bearer " + c.Secret, agentHeader: c.AgentID}, nil
}

func (Credentials) RequireTransportSecurity() bool { return true }

func authenticate(ctx context.Context, secret string) (context.Context, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	presented := md.Get(secretHeader)
	if len(presented) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing secret")
	}

	if !equalSecrets(strings.TrimPrefix(presented[0], "Bearer "), secret) {
		return nil, status.Error(codes.Unauthenticated, "wrong secret")
	}

	agents := md.Get(agentHeader)
	if len(agents) == 0 || agents[0] == "" {
		return nil, status.Error(codes.Unauthenticated, "missing agent id")
	}

	return WithPrincipal(ctx, &Principal{AgentID: agents[0]}), nil
}

func equalSecrets(a, b string) bool {
	x, y := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}

// UnaryInterceptor rejects calls that do not present secret and attaches
// the declared agent as the principal of the ones that do.
func UnaryInterceptor(secret string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		ctx, err := authenticate(ctx, secret)
		if err != nil {
			return nil, err
		}

		return handler(ctx, req)
	}
}

// StreamInterceptor is UnaryInterceptor for streaming calls.
func StreamInterceptor(secret string) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, err := authenticate(ss.Context(), secret)
		if err != nil {
			return err
		}

		return handler(srv, &stream{ServerStream: ss, ctx: ctx})
	}
}

type stream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *stream) Context() context.Context { return s.ctx }
