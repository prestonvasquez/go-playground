package goplayground

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type trCursor struct{}

func (c *trCursor) close() error {
	return nil
}

func TestRequire_Deref(t *testing.T) {
	var tcr *trCursor
	defer func() { require.NoError(t, tcr.close()) }()

	require.Nil(t, tcr)
}
