package storage

import (
	"context"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// cookies keeps what a store server sets, the way a browser would, and
// hands it back on later calls; a load balancer that pins a client to one
// instance by cookie, such as Cloud Run's session affinity, then keeps an
// agent on the instance holding its session.
type cookies struct {
	jar  *cookiejar.Jar
	base url.URL
}

func newCookies(addr string, secure bool) *cookies {
	jar, _ := cookiejar.New(nil)

	scheme := "http"
	if secure {
		scheme = "https"
	}

	return &cookies{jar: jar, base: url.URL{Scheme: scheme, Host: hostOf(addr)}}
}

// hostOf is the host a gRPC target names, without scheme or port.
func hostOf(target string) string {
	if i := strings.Index(target, ":///"); i >= 0 {
		target = target[i+4:]
	}

	if host, _, err := net.SplitHostPort(target); err == nil {
		return host
	}

	return target
}

func (c *cookies) url(method string) *url.URL {
	u := c.base
	u.Path = method

	return &u
}

func (c *cookies) attach(ctx context.Context, method string) context.Context {
	for _, cookie := range c.jar.Cookies(c.url(method)) {
		ctx = metadata.AppendToOutgoingContext(ctx, "cookie", cookie.String())
	}

	return ctx
}

func (c *cookies) keep(method string, header metadata.MD) {
	set := header.Get("set-cookie")
	if len(set) == 0 {
		return
	}

	var kept []*http.Cookie
	for _, line := range set {
		cookie, err := http.ParseSetCookie(line)
		if err == nil {
			kept = append(kept, cookie)
		}
	}

	c.jar.SetCookies(c.url(method), kept)
}

func (c *cookies) unary() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		var header metadata.MD
		err := invoker(c.attach(ctx, method), method, req, reply, cc, append(opts, grpc.Header(&header))...)
		c.keep(method, header)

		return err
	}
}

func (c *cookies) stream() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		s, err := streamer(c.attach(ctx, method), desc, cc, method, opts...)
		if err != nil {
			return nil, err
		}

		return &cookieStream{ClientStream: s, keep: func(header metadata.MD) { c.keep(method, header) }}, nil
	}
}

// cookieStream keeps the cookies of a stream's header once a message has
// arrived, when reading the header no longer blocks.
type cookieStream struct {
	grpc.ClientStream
	keep func(metadata.MD)
	once sync.Once
}

func (s *cookieStream) RecvMsg(m interface{}) error {
	err := s.ClientStream.RecvMsg(m)

	s.once.Do(func() {
		if header, herr := s.ClientStream.Header(); herr == nil {
			s.keep(header)
		}
	})

	return err
}
