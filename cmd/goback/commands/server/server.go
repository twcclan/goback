package server

import (
	"context"
	"crypto/tls"
	"errors"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/twcclan/goback/admin"
	"github.com/twcclan/goback/auth"
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/cmd/goback/commands/common"
	"github.com/twcclan/goback/cmd/goback/commands/gc"
	"github.com/twcclan/goback/index/sql"
	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/storage"
	"github.com/twcclan/goback/storage/maintenance"
	"github.com/twcclan/goback/storage/pack"
	"github.com/twcclan/goback/telemetry"

	"github.com/urfave/cli"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Command is the server command.
var Command = cli.Command{
	Action:      serverAction,
	Name:        "server",
	Description: "Run an object store server.",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "address",
			Value: ":6060",
		},
		cli.StringFlag{
			Name:  "secret",
			Usage: "the secret every agent of this server presents",
		},
		cli.StringFlag{
			Name:  "secret-file",
			Usage: "file holding the secret, for when a flag would show it",
		},
		cli.StringFlag{
			Name:  "tls-cert",
			Usage: "PEM certificate chain the server presents; with --tls-key, agents connect over TLS",
		},
		cli.StringFlag{
			Name:  "tls-key",
			Usage: "PEM private key for --tls-cert",
		},
		cli.BoolFlag{
			Name:  "plaintext-behind-proxy",
			Usage: "serve plaintext because a proxy in front of the server terminates TLS; never expose this listener directly",
		},
		cli.StringFlag{
			Name:  "admin-address",
			Usage: "listener for the operator surface (gRPC and REST); empty serves none",
		},
		cli.StringFlag{
			Name:  "admin-token",
			Usage: "bearer token the operator surface requires",
		},
		cli.DurationFlag{
			Name:  "retire-interval",
			Usage: "how often retired commits past their window are tombstoned; 0 disables the job",
			Value: time.Hour,
		},
		cli.DurationFlag{
			Name:  "gc-interval",
			Usage: "how often the store is garbage collected; 0 disables the job",
			Value: 7 * 24 * time.Hour,
		},
		cli.DurationFlag{
			Name:  "compact-interval",
			Usage: "how often small archives are compacted; 0 disables the job",
			Value: time.Hour,
		},
		cli.DurationFlag{
			Name:  "sweep-interval",
			Usage: "how often idle archives are finalized and expired sessions ended",
			Value: 30 * time.Second,
		},
		cli.DurationFlag{
			Name:  "presence-interval",
			Usage: "how often presence filters are built for new commits; 0 disables the job",
			Value: 30 * time.Second,
		},
	},
}

func serverAction(ctx *cli.Context) {
	secret, err := sharedSecret(ctx)
	if err != nil {
		log.Fatal(err)
	}

	creds, tlsConfig, err := transportCredentials(ctx)
	if err != nil {
		log.Fatal(err)
	}

	s := common.GetObjectStore(ctx)
	idx := common.OpenIndex(ctx, s)

	listener, err := net.Listen("tcp", ctx.String("address"))
	if err != nil {
		log.Fatal(err)
	}

	if _, err := telemetry.Setup(context.Background(), "goback"); err != nil {
		log.Fatalf("failed setting up telemetry: %s", err)
	}

	sessions, _ := s.(backup.SessionStore)
	if sessions == nil {
		log.Printf("Store %T has no sessions; uploads are visible as they arrive", s)
	}

	store := storage.NewStore(idx, sessions)

	if _, ok := idx.(backup.PresenceIndex); !ok {
		log.Printf("Index %T stores no presence filters; agents upload every new chunk", idx)
	}

	if _, ok := idx.(backup.RefScope); !ok {
		log.Printf("Index %T cannot scope reads; every ref is served", idx)
	}

	remote := storage.NewServer(store)

	srv := grpc.NewServer(
		grpc.Creds(creds),
		grpc.StatsHandler(otelgrpc.NewServerHandler(otelgrpc.WithPublicEndpoint())),
		grpc.ChainUnaryInterceptor(auth.UnaryInterceptor(secret), remote.UnaryInterceptor()),
		grpc.ChainStreamInterceptor(auth.StreamInterceptor(secret), remote.StreamInterceptor()),
	)

	proto.RegisterStoreServer(srv, remote)

	base := common.Unwrap(s)
	retirer, _ := idx.(backup.Retirer)
	collector, _ := base.(pack.Collector)
	runner := &maintenance.Runner{
		Retirer:   retirer,
		Collector: collector,
		OnCollect: gc.Log,
		Schedule: maintenance.Schedule{
			Sweep:    ctx.Duration("sweep-interval"),
			Compact:  ctx.Duration("compact-interval"),
			Collect:  ctx.Duration("gc-interval"),
			Retire:   ctx.Duration("retire-interval"),
			Presence: ctx.Duration("presence-interval"),
		},
	}
	runner.Store, _ = base.(maintenance.Store)
	runner.Presence, _ = idx.(maintenance.Presence)
	go runner.Run(context.Background())

	if addr := ctx.String("admin-address"); addr != "" {
		serveAdmin(addr, ctx.String("admin-token"), tlsConfig, idx, retirer, collector)
	}

	log.Println("Listening on", listener.Addr().String())
	log.Fatal(srv.Serve(listener))
}

// sharedSecret reads --secret or --secret-file; the server accepts no
// anonymous calls.
func sharedSecret(ctx *cli.Context) (string, error) {
	secret, file := ctx.String("secret"), ctx.String("secret-file")

	switch {
	case secret != "" && file != "":
		return "", errors.New("--secret and --secret-file are exclusive")
	case file != "":
		data, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}

		secret = strings.TrimSpace(string(data))
	}

	if secret == "" {
		return "", errors.New("--secret or --secret-file is required: the server accepts no anonymous calls")
	}

	return secret, nil
}

// transportCredentials serves TLS from --tls-cert and --tls-key, or
// plaintext when the operator declared a TLS-terminating proxy in front;
// the TLS configuration is nil for plaintext.
func transportCredentials(ctx *cli.Context) (credentials.TransportCredentials, *tls.Config, error) {
	cert, key := ctx.String("tls-cert"), ctx.String("tls-key")

	switch {
	case cert != "" && key != "":
		if ctx.Bool("plaintext-behind-proxy") {
			return nil, nil, errors.New("--plaintext-behind-proxy and --tls-cert are exclusive")
		}

		pair, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			return nil, nil, err
		}

		config := &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}

		return credentials.NewTLS(config), config, nil
	case cert != "" || key != "":
		return nil, nil, errors.New("--tls-cert and --tls-key go together")
	case ctx.Bool("plaintext-behind-proxy"):
		log.Println("Serving plaintext; a proxy must terminate TLS in front of this listener")
		return insecure.NewCredentials(), nil, nil
	default:
		return nil, nil, errors.New("--tls-cert and --tls-key are required unless --plaintext-behind-proxy is set")
	}
}

// serveAdmin starts the operator surface on addr with the main listener's
// TLS material.
func serveAdmin(addr, token string, tlsConfig *tls.Config, idx backup.Index, retirer backup.Retirer, collector pack.Collector) {
	if token == "" {
		log.Fatal("--admin-token is required with --admin-address")
	}

	x, ok := idx.(*sql.Index)
	if !ok {
		log.Fatalf("Index %T keeps no sets or policy; the admin surface needs one that does", idx)
	}

	server := &admin.Server{Index: x}

	if retirer != nil {
		server.RetireJob = func(ctx context.Context) (int, error) { return retirer.Retire(ctx, time.Now()) }
	}

	if collector != nil {
		server.CollectJob = func(ctx context.Context) (string, error) {
			report, err := collector.Collect(ctx, pack.CollectOptions{})
			if err != nil {
				return "", err
			}

			gc.Log(report)

			return gc.Summary(report), nil
		}
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}

	if tlsConfig != nil {
		config := tlsConfig.Clone()
		config.NextProtos = []string{"h2", "http/1.1"}
		listener = tls.NewListener(listener, config)
	}

	log.Println("Admin surface listening on", listener.Addr().String())

	go func() {
		log.Fatal(admin.NewHTTPServer(admin.Handler(token, server)).Serve(listener))
	}()
}
