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

func TestRequire_Deref(t *testing.T) {
	var tcr *trCursor

	if tcr != nil {
		defer func() { require.NoError(t, tcr.close()) }()
	}

	require.Nil(t, tcr)
}
