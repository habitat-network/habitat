package org

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// This is to satisfy test coverage
func TestPublicOrg(t *testing.T) {
	o := NewEveryoneOrg()
	ok, err := o.IsMember(context.Background(), "anything")
	require.NoError(t, err)
	require.True(t, ok)
}
