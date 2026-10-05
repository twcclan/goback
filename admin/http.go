package admin

import (
	"net/http"
	"strings"

	pb "github.com/twcclan/goback/proto/admin"

	"google.golang.org/grpc"
)

// Handler serves the admin service over gRPC on one listener; every call
// presents the token, and anything that is not a gRPC call is refused.
// Serve it with HTTP/2 enabled, see NewHTTPServer.
func Handler(token string, server pb.AdminServer) http.Handler {
	grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(UnaryInterceptor(token)))
	pb.RegisterAdminServer(grpcServer, server)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			http.Error(w, "the admin surface speaks gRPC only", http.StatusUnsupportedMediaType)

			return
		}

		grpcServer.ServeHTTP(w, r)
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
