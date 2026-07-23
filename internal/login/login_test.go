package login

import (
	"context"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
)

// --- pdsProvider ---

func TestPDSProvider_Authorize(t *testing.T) {
	clientMetadata := &oauth.ClientMetadata{
		RedirectURIs: []string{"https://pds.example.com/authorize"},
	}
	client := pdsclient.NewDummyOAuthClient(t, clientMetadata)
	defer client.Close()
	p := NewPDSProvider(client)

	redirect, _, err := p.Authorize(
		context.Background(),
		"did:web:pds.example.com",
	)
	require.NoError(t, err)
	require.Contains(t, redirect, "/authorize")
}

func TestPDSProvider_Authorize_EmptyHint(t *testing.T) {
	client := pdsclient.NewDummyOAuthClient(t, &oauth.ClientMetadata{})
	defer client.Close()
	p := NewPDSProvider(client)

	_, _, err := p.Authorize(context.Background(), "")
	require.Error(t, err)
}

func TestPDSProvider_Exchange(t *testing.T) {
	clientMetadata := &oauth.ClientMetadata{
		RedirectURIs: []string{"https://pds.example.com/authorize"},
	}
	client := pdsclient.NewDummyOAuthClient(t, clientMetadata)
	defer client.Close()
	p := NewPDSProvider(client)

	loginID, err := p.Exchange(
		t.Context(),
		url.Values{
			"code":  {"dummyCode"},
			"iss":   {"https://pds.example.com"},
			"state": {"dummyState"},
		},
		nil,
	)
	require.NoError(t, err)
	// from dummy oauth client
	require.Equal(t, pdsclient.DummyDID.String(), loginID)
}
