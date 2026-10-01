package pack

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestARewriteTrustsOnlyAReachableCopyOutsideItsGroup(t *testing.T) {
	rw := &rewrite{
		inGroup: map[string]bool{"candidate": true},
		group: &compactionGroup{marked: func(loc *IndexLocation) bool {
			return loc.Archive != "unreachable"
		}},
	}

	at := func(archives ...string) []IndexLocation {
		var copies []IndexLocation
		for _, archive := range archives {
			copies = append(copies, IndexLocation{Archive: archive})
		}

		return copies
	}

	require.False(t, rw.elsewhere(nil))
	require.False(t, rw.elsewhere(at("candidate")), "a copy the rewrite drops is no copy")
	require.False(t, rw.elsewhere(at("candidate", "unreachable")), "an unreachable copy may be swept next")
	require.True(t, rw.elsewhere(at("unreachable", "reachable")))
}
