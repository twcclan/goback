// Package health answers liveness and readiness probes, over HTTP at
// /healthz and /readyz and over gRPC as grpc.health.v1.Health.
//
// Liveness only says the process is up. Readiness runs every check added
// with Add and fails once Drain was called, so that a draining or
// disconnected instance is taken out of rotation without being restarted.
package health

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// checkTimeout bounds one readiness probe, all checks together.
const checkTimeout = 5 * time.Second

// Liveness is the gRPC service name that asks only whether the process
// is up; every other name, the empty one included, asks for readiness.
const Liveness = "liveness"

// ErrDraining is what readiness reports once the process is stopping.
var ErrDraining = errors.New("draining")

// A Check reports why the process cannot serve, or nil when it can.
type Check func(ctx context.Context) error

// Probe collects the checks of a process and answers probes with them.
// The zero value is ready to use.
type Probe struct {
	mu       sync.Mutex
	names    []string
	checks   []Check
	draining atomic.Bool
}

// Add makes readiness depend on check, reported under name.
func (p *Probe) Add(name string, check Check) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.names = append(p.names, name)
	p.checks = append(p.checks, check)
}

// Drain fails readiness from now on, for a process that is stopping.
func (p *Probe) Drain() {
	p.draining.Store(true)
}

// Ready runs every check and reports the ones that failed.
func (p *Probe) Ready(ctx context.Context) error {
	if p.draining.Load() {
		return ErrDraining
	}

	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	p.mu.Lock()
	names, checks := p.names, p.checks
	p.mu.Unlock()

	var errs []error

	for i, check := range checks {
		if err := check(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", names[i], err))
		}
	}

	return errors.Join(errs...)
}

// Handler serves /healthz and /readyz, and passes every other request to
// next, which may be nil to serve nothing else.
func (p *Probe) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			_, _ = w.Write([]byte("ok"))
		case "/readyz":
			if err := p.Ready(r.Context()); err != nil {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)

				return
			}

			_, _ = w.Write([]byte("ok"))
		default:
			if next == nil {
				http.NotFound(w, r)

				return
			}

			next.ServeHTTP(w, r)
		}
	})
}

// Register serves the probe on srv as grpc.health.v1.Health.
func (p *Probe) Register(srv *grpc.Server) {
	healthpb.RegisterHealthServer(srv, &grpcHealth{probe: p})
}

// Unary chains interceptors for every call but a health check, which a
// prober makes without credentials.
func Unary(interceptors ...grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if isProbe(info.FullMethod) {
			return handler(ctx, req)
		}

		for i := len(interceptors) - 1; i >= 0; i-- {
			interceptor, next := interceptors[i], handler
			handler = func(ctx context.Context, req any) (any, error) {
				return interceptor(ctx, req, info, next)
			}
		}

		return handler(ctx, req)
	}
}

// Stream is Unary for streaming calls.
func Stream(interceptors ...grpc.StreamServerInterceptor) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if isProbe(info.FullMethod) {
			return handler(srv, ss)
		}

		for i := len(interceptors) - 1; i >= 0; i-- {
			interceptor, next := interceptors[i], handler
			handler = func(srv any, ss grpc.ServerStream) error {
				return interceptor(srv, ss, info, next)
			}
		}

		return handler(srv, ss)
	}
}

func isProbe(method string) bool {
	return strings.HasPrefix(method, "/"+healthpb.Health_ServiceDesc.ServiceName+"/")
}

type grpcHealth struct {
	healthpb.UnimplementedHealthServer
	probe *Probe
}

func (g *grpcHealth) Check(ctx context.Context, req *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	if req.GetService() == Liveness {
		return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
	}

	if err := g.probe.Ready(ctx); err != nil {
		return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_NOT_SERVING}, nil
	}

	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
}
