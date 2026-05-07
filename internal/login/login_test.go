package login

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/stretchr/testify/require"
)

// stubOAuthClient is a minimal PdsOAuthClient for testing.
type stubOAuthClient struct {
	authorizeErr    error
	exchangeCodeErr error
	redirectURL     string
}

func (s *stubOAuthClient) ClientMetadata() *pdsclient.ClientMetadata { return nil }

func (s *stubOAuthClient) Authorize(_ *pdsclient.DpopHttpClient, _ *identity.Identity) (string, *pdsclient.AuthorizeState, error) {
	if s.authorizeErr != nil {
		return "", nil, s.authorizeErr
	}
	return s.redirectURL, &pdsclient.AuthorizeState{
		Verifier:      "verifier",
		State:         "state",
		TokenEndpoint: "https://pds.example.com/token",
	}, nil
}

func (s *stubOAuthClient) ExchangeCode(_ *pdsclient.DpopHttpClient, _, _ string, _ *pdsclient.AuthorizeState) (*pdsclient.TokenResponse, error) {
	if s.exchangeCodeErr != nil {
		return nil, s.exchangeCodeErr
	}
	return &pdsclient.TokenResponse{
		AccessToken:  "access",
		RefreshToken: "refresh",
	}, nil
}

func (s *stubOAuthClient) RefreshToken(_ *pdsclient.DpopHttpClient, _ *identity.Identity, _ string, _ string) (*pdsclient.TokenResponse, error) {
	return nil, errors.New("not used in these tests")
}

// stubCredStore tracks UpsertCredentials calls.
type stubCredStore struct {
	upserted  map[syntax.DID]*pdscred.Credentials
	upsertErr error
}

func newStubCredStore() *stubCredStore {
	return &stubCredStore{upserted: make(map[syntax.DID]*pdscred.Credentials)}
}

func (s *stubCredStore) UpsertCredentials(did syntax.DID, creds *pdscred.Credentials) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.upserted[did] = creds
	return nil
}

func (s *stubCredStore) GetCredentials(did syntax.DID) (*pdscred.Credentials, error) {
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

func idWithHabitatOnly() *identity.Identity {
	return &identity.Identity{
		DID: "did:web:habitat.example.com",
		Services: map[string]identity.ServiceEndpoint{
			"habitat": {URL: "https://habitat.example.com"},
		},
	}
}

func idWithBothServices() *identity.Identity {
	return &identity.Identity{
		DID: "did:web:both.example.com",
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {URL: "https://pds.example.com"},
			"habitat":     {URL: "https://habitat.example.com"},
		},
	}
}

func idWithNoServices() *identity.Identity {
	return &identity.Identity{
		DID:      "did:web:nobody.example.com",
		Services: map[string]identity.ServiceEndpoint{},
	}
}

// --- pdsProvider ---

func TestPDSProvider_CanHandle(t *testing.T) {
	p := NewPDSProvider(&stubOAuthClient{}, newStubCredStore())

	require.True(t, p.CanHandle(idWithPDSOnly()), "pds-only identity should be handled")
	require.False(t, p.CanHandle(idWithHabitatOnly()), "habitat-only identity should not be handled")
	require.False(t, p.CanHandle(idWithBothServices()), "identity with both services should not be handled")
	require.False(t, p.CanHandle(idWithNoServices()), "identity with no services should not be handled")
}

func TestPDSProvider_Authorize(t *testing.T) {
	client := &stubOAuthClient{redirectURL: "https://pds.example.com/authorize"}
	p := NewPDSProvider(client, newStubCredStore())

	redirect, state, err := p.Authorize(context.Background(), idWithPDSOnly())
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
	credStore := newStubCredStore()
	p := NewPDSProvider(&stubOAuthClient{redirectURL: "https://pds.example.com/authorize"}, credStore)
	did := syntax.DID("did:web:pds.example.com")

	// Obtain valid state from Authorize.
	_, state, err := p.Authorize(context.Background(), idWithPDSOnly())
	require.NoError(t, err)

	err = p.Exchange(context.Background(), did, "code", "https://pds.example.com", state)
	require.NoError(t, err)

	creds, stored := credStore.upserted[did]
	require.True(t, stored, "credentials should have been upserted")
	require.Equal(t, "access", creds.AccessToken)
	require.Equal(t, "refresh", creds.RefreshToken)
	require.NotNil(t, creds.DpopKey)
}

// --- habitatProvider ---

func TestHabitatProvider_CanHandle(t *testing.T) {
	p := NewHabitatProvider("frontend.test")

	require.True(t, p.CanHandle(idWithHabitatOnly()), "habitat-only identity should be handled")
	require.False(t, p.CanHandle(idWithPDSOnly()), "pds-only identity should not be handled")
	require.False(t, p.CanHandle(idWithBothServices()), "identity with both services should not be handled")
	require.False(t, p.CanHandle(idWithNoServices()), "identity with no services should not be handled")
}

func TestHabitatProvider_Authorize_AlwaysSucceeds(t *testing.T) {
	p := NewHabitatProvider("frontend.test")
	_, _, err := p.Authorize(context.Background(), idWithHabitatOnly())
	require.NoError(t, err)
}

func TestHabitatProvider_Exchange_AlwaysSucceeds(t *testing.T) {
	p := NewHabitatProvider("frontend.test")
	err := p.Exchange(context.Background(), "did:web:test", "code", "issuer", nil)
	require.NoError(t, err)
}

// --- Router ---

func newTestRouter() *Router {
	return NewRouter(
		NewPDSProvider(&stubOAuthClient{}, newStubCredStore()),
		NewHabitatProvider("frontend.test"),
	)
}

func TestRouter_For(t *testing.T) {
	r := newTestRouter()

	p, err := r.For(idWithPDSOnly())
	require.NoError(t, err)
	require.Equal(t, ProviderTypePDS, p.Type())

	p, err = r.For(idWithHabitatOnly())
	require.NoError(t, err)
	require.Equal(t, ProviderTypeHabitat, p.Type())

	_, err = r.For(idWithBothServices())
	require.Error(t, err, "identity with both services should have no provider")

	_, err = r.For(idWithNoServices())
	require.Error(t, err, "identity with no services should have no provider")
}

func TestRouter_ByType(t *testing.T) {
	r := newTestRouter()

	p, err := r.ByType(ProviderTypePDS)
	require.NoError(t, err)
	require.Equal(t, ProviderTypePDS, p.Type())

	p, err = r.ByType(ProviderTypeHabitat)
	require.NoError(t, err)
	require.Equal(t, ProviderTypeHabitat, p.Type())

	_, err = r.ByType("unknown")
	require.Error(t, err)
}

// unmarshalProviderState is a test helper to inspect the opaque pds state bytes.
func unmarshalProviderState(b []byte, s *pdsProviderState) error {
	return json.Unmarshal(b, s)
}
