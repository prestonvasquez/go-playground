package goplayground

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type trCursor struct {
	err error
}

func (c *trCursor) close() error {
	return c.err
}

func TestRequire_Defer(t *testing.T) {
	var tcr *trCursor

	if tcr != nil {
		defer func() {
			t.Log("closing cursor")
			require.NoError(t, tcr.close())
		}()
	}

	require.Nil(t, tcr)
}

func TestRequire_NoDefer(t *testing.T) {
	var tcr *trCursor

	require.Nil(t, tcr)
	require.True(t, false)

	if tcr != nil {
		defer func() {
			t.Log("closing cursor")
			require.NoError(t, tcr.close())
		}()
	}
}
