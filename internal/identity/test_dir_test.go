package identity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	habitat_identity "github.com/habitat-network/habitat/internal/identity"
)

type failDir struct{ identity.Directory }

func (failDir) LookupHandle(context.Context, syntax.Handle) (*identity.Identity, error) {
	return nil, errors.New("base")
}

func (failDir) LookupDID(context.Context, syntax.DID) (*identity.Identity, error) {
	return nil, errors.New("base")
}

func TestTestDirectory(t *testing.T) {
	ctx := context.Background()
	dir := habitat_identity.NewTestDirectory(failDir{})

	ident, err := dir.LookupHandle(ctx, "Alice.test")
	require.NoError(t, err)
	require.Equal(t, syntax.DID("did:web:alice.test"), ident.DID)
	require.Equal(t, syntax.Handle("alice.test"), ident.Handle)

	ident, err = dir.LookupDID(ctx, "did:web:alice.test")
	require.NoError(t, err)
	require.Equal(t, syntax.Handle("alice.test"), ident.Handle)

	atid, err := syntax.ParseAtIdentifier("bob.test")
	require.NoError(t, err)
	ident, err = dir.Lookup(ctx, atid)
	require.NoError(t, err)
	require.Equal(t, syntax.DID("did:web:bob.test"), ident.DID)

	// Everything else goes to the base directory.
	_, err = dir.LookupHandle(ctx, "alice.bsky.social")
	require.EqualError(t, err, "base")
	_, err = dir.LookupDID(ctx, "did:web:example.com")
	require.EqualError(t, err, "base")
	_, err = dir.LookupDID(ctx, "did:plc:abc")
	require.EqualError(t, err, "base")
}
