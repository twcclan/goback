package admin

import (
	"context"
	"net/http"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
)

// Service is an admin implementation that registers itself with a gRPC
// server and with the REST gateway in front of it.
type Service interface {
	Register(*grpc.Server)
	RegisterGateway(context.Context, *runtime.ServeMux) error
}

// Handler serves an admin service on one listener: gRPC requests go to
// the gRPC server, everything else to the REST gateway in front of the
// same implementation; both require the token. Serve it with HTTP/2
// enabled, see NewHTTPServer.
func Handler(token string, service Service) http.Handler {
	grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(UnaryInterceptor(token)))
	service.Register(grpcServer)

	gateway := runtime.NewServeMux()
	// the gateway calls the implementation directly, so the token is
	// checked here instead of by the interceptor
	_ = service.RegisterGateway(context.Background(), gateway)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			grpcServer.ServeHTTP(w, r)
			return
		}

		presented := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !Authorized(map[string][]string{header: {presented}}, token) {
			http.Error(w, "admin token required", http.StatusUnauthorized)
			return
		}

		gateway.ServeHTTP(w, r)
	})
}

// NewHTTPServer returns a server for Handler that speaks HTTP/1.1 and
// HTTP/2 with or without TLS, so gRPC works behind a plaintext proxy too.
func NewHTTPServer(handler http.Handler) *http.Server {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(true)
	protocols.SetUnencryptedHTTP2(true)

	return &http.Server{Handler: handler, Protocols: protocols}
}
