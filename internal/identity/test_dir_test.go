package identity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
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
	hostKey, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	dir := habitat_identity.NewTestDirectory(failDir{}, hostKey, "pear.example.com")

	ident, err := dir.LookupHandle(ctx, "Alice.test")
	require.NoError(t, err)
	// The published key is the one the host derives and signs with.
	signer, err := habitat_identity.TestSigningKey(hostKey, "did:web:alice.test")
	require.NoError(t, err)
	pub, err := signer.PublicKey()
	require.NoError(t, err)
	require.Equal(t, pub.Multibase(), ident.Keys["atproto"].PublicKeyMultibase)
	require.Equal(t, syntax.DID("did:web:alice.test"), ident.DID)
	require.Equal(t, syntax.Handle("alice.test"), ident.Handle)
	// pear is its PDS, so atproto OAuth for it runs against pear.
	require.Equal(t, "https://pear.example.com", ident.PDSEndpoint())

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
