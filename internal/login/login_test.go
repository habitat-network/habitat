package login

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"net/url"
	"testing"

	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/keyval"
	keyval_testutil "github.com/habitat-network/habitat/internal/keyval/testutil"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/stretchr/testify/require"
)

// --- pdsProvider ---

func TestPDSProvider_Authorize(t *testing.T) {
	credStore, err := pdscred.NewPDSCredentialStore(
		testutil.NewDB(t, pdscred.Models()),
		encrypt.TestKey,
	)
	require.NoError(t, err)
	clientMetadata := &pdsclient.ClientMetadata{
		RedirectUris: []string{"https://pds.example.com/authorize"},
	}
	client := pdsclient.NewDummyOAuthClient(t, clientMetadata)
	defer client.Close()
	kv := keyval_testutil.New(t)
	p := NewPDSProvider(
		client,
		credStore,
		pdsclient.NewDummyDirectory("https://pds.example.com"),
		kv,
	)

	redirect, _, key, err := p.Authorize(
		context.Background(),
		"did:web:pds.example.com",
	)
	require.NoError(t, err)
	require.Contains(t, redirect, "/authorize")
	require.NotEmpty(t, key)

	// The state is stored server-side under the returned key, and the DPoP
	// private key's bytes survive the JSON round trip unchanged.
	var s pdsProviderState
	require.NoError(t, kv.Get(t.Context(), key, &s))
	require.Equal(t, "dummyVerifier", s.AuthorizeState.Verifier)
	dpopKey, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), s.DpopKey)
	require.NoError(t, err)
	roundTripped, err := dpopKey.Bytes()
	require.NoError(t, err)
	require.Equal(t, s.DpopKey, roundTripped)
}

func TestPDSProvider_Exchange(t *testing.T) {
	credStore, err := pdscred.NewPDSCredentialStore(
		testutil.NewDB(t, pdscred.Models()),
		encrypt.TestKey,
	)
	require.NoError(t, err)
	clientMetadata := &pdsclient.ClientMetadata{
		RedirectUris: []string{"https://pds.example.com/authorize"},
	}
	client := pdsclient.NewDummyOAuthClient(t, clientMetadata)
	defer client.Close()
	p := NewPDSProvider(
		client,
		credStore,
		pdsclient.NewDummyDirectory("https://pds.example.com"),
		keyval_testutil.New(t),
	)

	// Obtain valid state from Authorize.
	_, _, key, err := p.Authorize(
		t.Context(),
		"did:web:pds.example.com",
	)
	require.NoError(t, err)

	loginID, _, err := p.Exchange(
		t.Context(),
		url.Values{"code": {"dummyCode"}, "iss": {"https://pds.example.com"}},
		key,
	)
	require.NoError(t, err)
	// from dummy oauth client
	require.Equal(t, "did:web:example.did.com", loginID)

	creds, err := credStore.GetCredentials(t.Context(), "did:web:example.did.com")
	require.NoError(t, err)
	require.Equal(t, "dummy_refresh_token", creds.RefreshToken)
	require.NotNil(t, creds.DpopKey)

	// The state is single-use: replaying the same callback must fail.
	_, _, err = p.Exchange(
		t.Context(),
		url.Values{"code": {"dummyCode"}, "iss": {"https://pds.example.com"}},
		key,
	)
	require.ErrorIs(t, err, keyval.ErrNotFound)
}
