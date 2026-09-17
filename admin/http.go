package admin

import (
	"context"
	"net/http"
	"strings"

	pb "github.com/twcclan/goback/proto/admin"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
)

// Handler serves the admin surface on one listener: gRPC requests go to
// the gRPC server, everything else to the REST gateway in front of the
// same implementation; both require the token. Serve it with HTTP/2
// enabled, see NewHTTPServer.
func Handler(server *Server, token string) http.Handler {
	grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(UnaryInterceptor(token)))
	pb.RegisterAdminServer(grpcServer, server)

	gateway := runtime.NewServeMux()
	// the gateway calls the implementation directly, so the token is
	// checked here instead of by the interceptor
	_ = pb.RegisterAdminHandlerServer(context.Background(), gateway, server)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			grpcServer.ServeHTTP(w, r)
			return
		}

		presented := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !authorized(map[string][]string{header: {presented}}, token) {
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
