package login_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/login"
)

func TestPassthroughProvider(t *testing.T) {
	p := login.NewPassthroughProvider("https://pear.example.com/oauth-callback")
	redirect, state, key, err := p.Authorize(context.Background(), "did:plc:abc")
	require.NoError(t, err)
	require.NotEmpty(t, state)
	require.Empty(t, key)

	u, err := url.Parse(redirect)
	require.NoError(t, err)
	require.Equal(t, "pear.example.com", u.Host)
	require.Equal(t, "/oauth-callback", u.Path)
	require.Equal(t, state, u.Query().Get("state"))

	id, _, err := p.Exchange(context.Background(), u.Query(), key)
	require.NoError(t, err)
	require.Equal(t, "did:plc:abc", id)
}

type handleDir struct {
	identity.Directory
	handle syntax.Handle
}

func (d handleDir) LookupDID(_ context.Context, did syntax.DID) (*identity.Identity, error) {
	return &identity.Identity{DID: did, Handle: d.handle}, nil
}

func TestRouterPassthroughOnlyForTestHandles(t *testing.T) {
	pds := &stubProvider{}
	pass := login.NewPassthroughProvider("https://pear.example.com/oauth-callback")
	did := syntax.DID("did:plc:abc")

	r := &login.Router{
		Pds:         pds,
		Passthrough: pass,
		Directory:   handleDir{handle: "alice.test"},
	}
	redirect, _, _, err := r.Authorize(context.Background(), did)
	require.NoError(t, err)
	require.Contains(t, redirect, "pear.example.com/oauth-callback")
	require.Empty(t, pds.loginHint)

	r = &login.Router{
		Pds:         pds,
		Passthrough: pass,
		Directory:   handleDir{handle: "alice.bsky.social"},
	}
	redirect, _, _, err = r.Authorize(context.Background(), did)
	require.NoError(t, err)
	require.Equal(t, "https://provider.example.com/authorize", redirect)
	require.Equal(t, did.String(), pds.loginHint)
}

func TestRouterPassthroughForDidWebTest(t *testing.T) {
	pds := &stubProvider{}
	pass := login.NewPassthroughProvider("https://pear.example.com/oauth-callback")
	r := &login.Router{Pds: pds, Passthrough: pass}

	redirect, _, _, err := r.Authorize(context.Background(), "did:web:alice.test")
	require.NoError(t, err)
	require.Contains(t, redirect, "pear.example.com/oauth-callback")
	require.Empty(t, pds.loginHint)

	_, _, _, err = r.Authorize(context.Background(), "did:web:example.com")
	require.NoError(t, err)
	require.Equal(t, "did:web:example.com", pds.loginHint)
}
