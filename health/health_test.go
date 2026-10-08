package health_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gobackio/goback/health"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	return rec.Code, rec.Body.String()
}

func TestAFailingCheckFailsReadinessButNotLiveness(t *testing.T) {
	var probe health.Probe
	probe.Add("database", func(context.Context) error { return errors.New("connection refused") })

	h := probe.Handler(nil)

	code, _ := get(t, h, "/healthz")
	require.Equal(t, http.StatusOK, code, "a process whose database is away is not one to restart")

	code, body := get(t, h, "/readyz")
	require.Equal(t, http.StatusServiceUnavailable, code)
	require.Contains(t, body, "database: connection refused")
}

func TestADrainingProcessIsNotReady(t *testing.T) {
	var probe health.Probe

	h := probe.Handler(nil)

	code, _ := get(t, h, "/readyz")
	require.Equal(t, http.StatusOK, code)

	probe.Drain()

	code, _ = get(t, h, "/readyz")
	require.Equal(t, http.StatusServiceUnavailable, code)

	code, _ = get(t, h, "/healthz")
	require.Equal(t, http.StatusOK, code)
}

func TestEveryOtherPathGoesToTheHandlerBehind(t *testing.T) {
	var probe health.Probe

	h := probe.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	code, _ := get(t, h, "/v1/sets")
	require.Equal(t, http.StatusTeapot, code)
}

// A server whose interceptors refuse everything still answers a prober,
// which has no credentials to present.
func TestAHealthCheckSkipsTheServersInterceptors(t *testing.T) {
	var probe health.Probe

	var failing atomic.Bool

	probe.Add("database", func(context.Context) error {
		if failing.Load() {
			return errors.New("connection refused")
		}

		return nil
	})

	refuse := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		return nil, status.Error(codes.Unauthenticated, "no credentials")
	}
	refuseStream := func(any, grpc.ServerStream, *grpc.StreamServerInfo, grpc.StreamHandler) error {
		return status.Error(codes.Unauthenticated, "no credentials")
	}

	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(health.Unary(refuse)),
		grpc.ChainStreamInterceptor(health.Stream(refuseStream)),
	)
	probe.Register(srv)
	reflection.Register(srv)

	listener := bufconn.Listen(1 << 20)

	go func() { _ = srv.Serve(listener) }()

	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///probe",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	t.Cleanup(func() { _ = conn.Close() })

	client := healthpb.NewHealthClient(conn)
	ctx := context.Background()

	answer, err := client.Check(ctx, &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	require.Equal(t, healthpb.HealthCheckResponse_SERVING, answer.Status)

	failing.Store(true)

	answer, err = client.Check(ctx, &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	require.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, answer.Status)

	answer, err = client.Check(ctx, &healthpb.HealthCheckRequest{Service: health.Liveness})
	require.NoError(t, err)
	require.Equal(t, healthpb.HealthCheckResponse_SERVING, answer.Status)

	stream, err := reflectionpb.NewServerReflectionClient(conn).ServerReflectionInfo(ctx)
	require.NoError(t, err)

	_, err = stream.Recv()
	require.Equal(t, codes.Unauthenticated, status.Code(err), "only the health service is exempt")
}
