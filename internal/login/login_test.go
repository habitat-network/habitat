package login

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/stretchr/testify/require"
)

// helpers to build identities with specific service combinations

func idWithPDSOnly() *identity.Identity {
	return &identity.Identity{
		DID: "did:web:pds.example.com",
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {URL: "https://pds.example.com"},
		},
	}
}

func TestPDSProvider_LoginMethod(t *testing.T) {
	p := NewPDSProvider(nil, nil)
	require.Equal(t, org.LoginMethodAtproto, p.LoginMethod())
}

func TestPDSProvider_Authorize_Error(t *testing.T) {
	p := NewPDSProvider(nil, nil)
	_, _, err := p.Authorize(context.Background(), idWithPDSOnly(), "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "oauth client not configured")
}

func TestPDSProvider_Exchange_Error(t *testing.T) {
	p := NewPDSProvider(nil, nil)
	err := p.Exchange(context.Background(), "did:web:test.com", "code", "iss", "state", []byte("{}"))
	require.Error(t, err)
}

// dummyProvider is a test stand-in for any non-PDS provider.
type dummyProvider struct{}

func NewDummyProvider() Provider { return &dummyProvider{} }

func (d *dummyProvider) LoginMethod() org.LoginMethod { return org.LoginMethodPassword }

func (d *dummyProvider) Authorize(
	_ context.Context,
	_ *identity.Identity,
	_ string,
) (string, []byte, error) {
	return "https://dummy.example.com/login", nil, nil
}
func (d *dummyProvider) Exchange(_ context.Context, _ syntax.DID, _, _ string, _ string, _ []byte) error {
	return nil
}

// --- Router ---

func TestRouter_ByLoginMethod(t *testing.T) {
	r := NewRouter(
		NewPDSProvider(nil, nil),
		NewDummyProvider(),
	)

	p, err := r.ByLoginMethod(org.LoginMethodAtproto)
	require.NoError(t, err)
	require.Equal(t, org.LoginMethodAtproto, p.LoginMethod())

	p, err = r.ByLoginMethod(org.LoginMethodPassword)
	require.NoError(t, err)
	require.Equal(t, org.LoginMethodPassword, p.LoginMethod())

	_, err = r.ByLoginMethod("unknown")
	require.Error(t, err)
}

// unmarshalProviderState is a test helper to inspect the opaque pds state bytes.
func unmarshalProviderState(b []byte, s *pdsProviderState) error {
	return json.Unmarshal(b, s)
}
