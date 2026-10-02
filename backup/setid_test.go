package backup

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetID(t *testing.T) {
	for s, want := range map[string]uint64{"7": 7, "0042": 42, "99999999999999999999": 0} {
		id, ok := SetID(s)
		require.True(t, ok, s)
		require.Equal(t, want, id, s)
	}

	for _, s := range []string{"", "control-base", "7a", "-7", "+7", " 7"} {
		_, ok := SetID(s)
		require.False(t, ok, s)
	}
}
