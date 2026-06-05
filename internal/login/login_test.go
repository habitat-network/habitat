package login

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// stubOAuthClient is a minimal PdsOAuthClient for testing.
type stubOAuthClient struct {
	authorizeErr    error
	exchangeCodeErr error
	redirectURL     string
}

func (s *stubOAuthClient) ClientMetadata() *pdsclient.ClientMetadata { return nil }

func (s *stubOAuthClient) Authorize(
	_ context.Context,
	_ *pdsclient.DpopHttpClient,
	_ *identity.Identity,
) (string, *pdsclient.AuthorizeState, error) {
	if s.authorizeErr != nil {
		return "", nil, s.authorizeErr
	}
	return s.redirectURL, &pdsclient.AuthorizeState{
		Verifier:      "verifier",
		State:         "state",
		TokenEndpoint: "https://pds.example.com/token",
	}, nil
}

func (s *stubOAuthClient) ExchangeCode(
	_ *pdsclient.DpopHttpClient,
	_, _ string,
	_ *pdsclient.AuthorizeState,
) (*pdsclient.TokenResponse, error) {
	if s.exchangeCodeErr != nil {
		return nil, s.exchangeCodeErr
	}
	return &pdsclient.TokenResponse{
		AccessToken:  "access",
		RefreshToken: "refresh",
	}, nil
}

func (s *stubOAuthClient) RefreshToken(
	_ *pdsclient.DpopHttpClient,
	_ *identity.Identity,
	_ string,
	_ string,
) (*pdsclient.TokenResponse, error) {
	return nil, errors.New("not used in these tests")
}

// helpers to build identities with specific service combinations

func idWithPDSOnly() *identity.Identity {
	return &identity.Identity{
		DID: "did:web:pds.example.com",
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {URL: "https://pds.example.com"},
		},
	}
}

// --- pdsProvider ---

func TestPDSProvider_Authorize(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	credStore, err := pdscred.NewPDSCredentialStore(db, encrypt.TestKey)
	require.NoError(t, err)
	client := &stubOAuthClient{redirectURL: "https://pds.example.com/authorize"}
	p := NewPDSProvider(
		client,
		credStore,
		pdsclient.NewDummyDirectory("https://pds.example.com"),
	)

	redirect, state, err := p.Authorize(
		context.Background(),
		"did:web:pds.example.com",
		"did:plc:publicdid",
	)
	require.NoError(t, err)
	require.Equal(t, "https://pds.example.com/authorize", redirect)
	require.NotEmpty(t, state)

	// state must round-trip through Exchange — verify it's valid JSON with expected fields
	var s pdsProviderState
	require.NoError(t, unmarshalProviderState(state, &s))
	require.NotEmpty(t, s.DpopKey)
	require.Equal(t, "verifier", s.AuthorizeState.Verifier)
}

func TestPDSProvider_Exchange(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	credStore, err := pdscred.NewPDSCredentialStore(db, encrypt.TestKey)
	require.NoError(t, err)
	p := NewPDSProvider(
		&stubOAuthClient{redirectURL: "https://pds.example.com/authorize"},
		credStore,
		pdsclient.NewDummyDirectory("https://pds.example.com"),
	)

	// Obtain valid state from Authorize.
	_, state, err := p.Authorize(
		context.Background(),
		"did:web:pds.example.com",
		"did:plc:publicdid",
	)
	require.NoError(t, err)

	err = p.Exchange(
		context.Background(),
		"did:web:pds.example.com",
		"did:plc:publicdid",
		"code",
		"https://pds.example.com",
		state,
	)
	require.NoError(t, err)

	creds, err := credStore.GetCredentials(t.Context(), "did:web:pds.example.com")
	require.NoError(t, err)
	require.Equal(t, "access", creds.AccessToken)
	require.Equal(t, "refresh", creds.RefreshToken)
	require.NotNil(t, creds.DpopKey)
}

// unmarshalProviderState is a test helper to inspect the opaque pds state bytes.
func unmarshalProviderState(b []byte, s *pdsProviderState) error {
	return json.Unmarshal(b, s)
}
