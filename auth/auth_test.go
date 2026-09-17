package auth

import (
	"context"
	"testing"

	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAuthorizeCommit(t *testing.T) {
	agent := &Principal{AgentID: "node-7"}

	require.NoError(t, agent.AuthorizeCommit(&proto.Commit{AgentId: "node-7", BackupSet: "world"}))
	require.ErrorIs(t, agent.AuthorizeCommit(&proto.Commit{AgentId: "node-8", BackupSet: "world"}), ErrForbidden)
	require.ErrorIs(t, agent.AuthorizeCommit(&proto.Commit{BackupSet: "world"}), ErrForbidden)

	// a principal without an agent accepts any
	require.NoError(t, (&Principal{}).AuthorizeCommit(&proto.Commit{AgentId: "anything"}))
}

func TestInterceptorAttachesPrincipal(t *testing.T) {
	incoming := func(c Credentials) context.Context {
		md, err := c.GetRequestMetadata(context.Background())
		require.NoError(t, err)

		return metadata.NewIncomingContext(context.Background(), metadata.New(md))
	}

	var seen *Principal
	record := func(ctx context.Context, _ interface{}) (interface{}, error) {
		seen, _ = FromContext(ctx)
		return nil, nil
	}
	refuse := func(context.Context, interface{}) (interface{}, error) {
		t.Fatal("handler must not run")
		return nil, nil
	}

	_, err := UnaryInterceptor("s3cret")(incoming(Credentials{Secret: "s3cret", AgentID: "node-7"}), nil, nil, record)
	require.NoError(t, err)
	require.Equal(t, &Principal{AgentID: "node-7"}, seen)

	_, err = UnaryInterceptor("s3cret")(incoming(Credentials{Secret: "wrong", AgentID: "node-7"}), nil, nil, refuse)
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	_, err = UnaryInterceptor("s3cret")(incoming(Credentials{Secret: "s3cret"}), nil, nil, refuse)
	require.Equal(t, codes.Unauthenticated, status.Code(err), "an agent must say who it is")

	_, err = UnaryInterceptor("s3cret")(context.Background(), nil, nil, refuse)
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	_, err = Require(context.Background())
	require.ErrorIs(t, err, ErrUnauthenticated)
}
