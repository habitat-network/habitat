package org

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

func TestEveryoneOrg_IsMember(t *testing.T) {
	o := NewEveryoneOrg()
	ok, err := o.IsMember(context.Background(), "did:plc:test")
	require.NoError(t, err)
	require.True(t, ok)
}

func TestEveryoneOrg_LoginMethod(t *testing.T) {
	o := NewEveryoneOrg()
	require.Equal(t, "atproto", o.LoginMethod())
}

func TestEveryoneOrg_ErrorMethods(t *testing.T) {
	o := NewEveryoneOrg()
	ctx := context.Background()
	did := syntax.DID("did:plc:test")

	err := o.AddAdmin(ctx, did)
	require.ErrorIs(t, err, ErrNotSupportedPublic)

	_, err = o.GetAdmins(ctx)
	require.ErrorIs(t, err, ErrNotSupportedPublic)

	_, err = o.GetMembers(ctx)
	require.ErrorIs(t, err, ErrNotSupportedPublic)

	_, err = o.IsAdmin(ctx, did)
	require.ErrorIs(t, err, ErrNotSupportedPublic)

	err = o.RemoveAdmin(ctx, did)
	require.ErrorIs(t, err, ErrNotSupportedPublic)

	err = o.RemoveMembers(ctx, []syntax.DID{did})
	require.ErrorIs(t, err, ErrNotSupportedPublic)
}
