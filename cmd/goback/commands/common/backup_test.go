package common

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemoteAddressKeepsThePortOnlyOnce(t *testing.T) {
	for _, test := range []struct{ raw, want string }{
		{"goback://store.example", "store.example:6060"},
		{"goback://store.example:8443", "store.example:8443"},
		{"goback://127.0.0.1:8443", "127.0.0.1:8443"},
		{"goback://[::1]:8443", "[::1]:8443"},
	} {
		u, err := url.Parse(test.raw)
		require.NoError(t, err)
		require.Equal(t, test.want, remoteAddress(u), test.raw)
	}
}
