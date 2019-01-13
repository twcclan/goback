package rice2migrate

import (
	"testing"

	"github.com/GeertJohan/go.rice"
	st "github.com/golang-migrate/migrate/v4/source/testing"
	"github.com/stretchr/testify/require"
)

func TestRice(t *testing.T) {
	box, err := rice.FindBox("testdata")
	require.Nil(t, err)

	driver, err := WithInstance(box)
	require.Nil(t, err)

	st.Test(t, driver)
}
