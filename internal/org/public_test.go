package org_test

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/org"
)

func TestEveryoneOrg_IsMember(t *testing.T) {
	o := org.NewEveryoneOrg("everyone.example.com")
	ok, err := o.IsMember(context.Background(), "did:plc:test")
	require.NoError(t, err)
	require.True(t, ok)
}

func TestEveryoneOrg_ErrorMethods(t *testing.T) {
	o := org.NewEveryoneOrg("everyone.example.com")
	ctx := context.Background()
	did := syntax.DID("did:plc:test")

	err := o.AddAdmin(ctx, did)
	require.ErrorIs(t, err, org.ErrNotSupportedPublic)

	_, err = o.GetAdmins(ctx)
	require.ErrorIs(t, err, org.ErrNotSupportedPublic)

	_, err = o.GetMembers(ctx)
	require.ErrorIs(t, err, org.ErrNotSupportedPublic)

	_, err = o.IsAdmin(ctx, did)
	require.ErrorIs(t, err, org.ErrNotSupportedPublic)

	err = o.RemoveAdmin(ctx, did)
	require.ErrorIs(t, err, org.ErrNotSupportedPublic)

	err = o.RemoveMembers(ctx, []syntax.DID{did})
	require.ErrorIs(t, err, org.ErrNotSupportedPublic)
}
