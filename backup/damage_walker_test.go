package backup

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestADamagedPathIsReadAgainThoughNothingChanged(t *testing.T) {
	f := newWalkerFixture(t)
	f.write("a.bin", []byte("the file that lost a block"))
	f.write("b.bin", []byte("the file that did not"))

	first := f.run()
	require.EqualValues(t, 2, first.Read)

	second := f.run()
	require.EqualValues(t, 2, second.Reused, "nothing changed, so nothing is read")
	require.EqualValues(t, 0, second.Read)

	f.index.damaged = []string{"a.bin"}

	third := f.run()
	require.EqualValues(t, 1, third.Read, "the damaged path is read again")
	require.EqualValues(t, 1, third.Reused, "the rest keeps its skip")
}

func TestARescanReadsEveryFileAgain(t *testing.T) {
	f := newWalkerFixture(t)
	f.write("a.bin", []byte("one"))
	f.write("b.bin", []byte("two"))

	f.run()
	require.EqualValues(t, 2, f.run().Reused)

	f.index.rescan = true

	result := f.run()
	require.EqualValues(t, 2, result.Read)
	require.EqualValues(t, 0, result.Reused)
}
