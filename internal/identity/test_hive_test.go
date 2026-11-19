package identity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/hive"
	habitat_identity "github.com/habitat-network/habitat/internal/identity"
)

type errHive struct{ hive.Hive }

func (errHive) PrivateKeyForDID(context.Context, syntax.DID) (atcrypto.PrivateKey, error) {
	return nil, errors.New("base")
}

func TestTestHiveSignsForTestDIDs(t *testing.T) {
	hostKey, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	h := habitat_identity.NewTestHive(errHive{}, hostKey)

	got, err := h.PrivateKeyForDID(context.Background(), "did:web:alice.test")
	require.NoError(t, err)
	want, err := habitat_identity.TestSigningKey(hostKey, "did:web:alice.test")
	require.NoError(t, err)
	gotPub, err := got.PublicKey()
	require.NoError(t, err)
	wantPub, err := want.PublicKey()
	require.NoError(t, err)
	require.Equal(t, wantPub.Multibase(), gotPub.Multibase())

	_, err = h.PrivateKeyForDID(context.Background(), "did:web:abc.members.example.com")
	require.EqualError(t, err, "base")
}
