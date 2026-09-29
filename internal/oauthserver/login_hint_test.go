package oauthserver

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/emaildomain"
)

// fakeEmailResolver resolves a fixed set of emails.
type fakeEmailResolver map[emaildomain.Email]syntax.DID

func (f fakeEmailResolver) ResolveEmailIdentity(
	ctx context.Context,
	email emaildomain.Email,
) (*identity.Identity, error) {
	did, ok := f[email]
	if !ok {
		return nil, identity.ErrDIDNotFound
	}
	return &identity.Identity{DID: did}, nil
}

func TestResolveLoginHintEmail(t *testing.T) {
	alice := syntax.DID("did:web:alice.example.com")
	o := &OAuthServer{
		directory:     identity.NewMockDirectory(),
		emailResolver: fakeEmailResolver{"alice@acme.com": alice},
	}

	did, err := o.resolveLoginHint(t.Context(), "Alice@acme.com")
	require.NoError(t, err)
	require.Equal(t, alice, did)

	_, err = o.resolveLoginHint(t.Context(), "bob@other.com")
	require.ErrorIs(t, err, identity.ErrDIDNotFound)

	// Without a resolver, emails keep resolving to no subject, as before.
	o.emailResolver = nil
	did, err = o.resolveLoginHint(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	require.Empty(t, did)
}
