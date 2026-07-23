package login

import (
	"context"
	"encoding/json"
	"testing"

	habitatdb "github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/stretchr/testify/require"
)

// --- pdsProvider ---

func TestPDSProvider_Authorize(t *testing.T) {
	db := testutil.NewDB(t)
	credStore, err := pdscred.NewPDSCredentialStore(db, encrypt.TestKey)
	require.NoError(t, err)
	require.NoError(t, habitatdb.AutoMigrate(t.Context(), db, credStore))
	clientMetadata := &pdsclient.ClientMetadata{
		RedirectUris: []string{"https://pds.example.com/authorize"},
	}
	client := pdsclient.NewDummyOAuthClient(t, clientMetadata)
	defer client.Close()
	p := NewPDSProvider(
		client,
		credStore,
		pdsclient.NewDummyDirectory("https://pds.example.com"),
	)

	redirect, state, err := p.Authorize(
		context.Background(),
		"did:web:pds.example.com",
	)
	require.NoError(t, err)
	require.Contains(t, redirect, "/authorize")
	require.NotEmpty(t, state)

	// state must round-trip through Exchange — verify it's valid JSON with expected fields
	var s pdsProviderState
	require.NoError(t, unmarshalProviderState(state, &s))
	require.NotEmpty(t, s.DpopKey)
	require.Equal(t, "dummyVerifier", s.AuthorizeState.Verifier)
}

func TestPDSProvider_Exchange(t *testing.T) {
	db := testutil.NewDB(t)
	credStore, err := pdscred.NewPDSCredentialStore(db, encrypt.TestKey)
	require.NoError(t, err)
	require.NoError(t, habitatdb.AutoMigrate(t.Context(), db, credStore))
	clientMetadata := &pdsclient.ClientMetadata{
		RedirectUris: []string{"https://pds.example.com/authorize"},
	}
	client := pdsclient.NewDummyOAuthClient(t, clientMetadata)
	defer client.Close()
	p := NewPDSProvider(
		client,
		credStore,
		pdsclient.NewDummyDirectory("https://pds.example.com"),
	)

	// Obtain valid state from Authorize.
	_, state, err := p.Authorize(
		t.Context(),
		"did:web:pds.example.com",
	)
	require.NoError(t, err)

	loginID, err := p.Exchange(
		t.Context(),
		"dummyCode",
		"https://pds.example.com",
		state,
	)
	require.NoError(t, err)
	// from dummy oauth client
	require.Equal(t, "did:web:example.did.com", loginID)

	creds, err := credStore.GetCredentials(t.Context(), "did:web:example.did.com")
	require.NoError(t, err)
	require.Equal(t, "dummy_refresh_token", creds.RefreshToken)
	require.NotNil(t, creds.DpopKey)
}

// unmarshalProviderState is a test helper to inspect the opaque pds state bytes.
func unmarshalProviderState(b []byte, s *pdsProviderState) error {
	return json.Unmarshal(b, s)
}
