package testutil

import (
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

// defaultScopes is what an actor's token grants unless a test asks for
// something narrower. "org:*" satisfies every org-scoped requirement, so tests
// that are not about scope enforcement do not have to think about it; tests
// that are should use ActorWithScopes.
var defaultScopes = []string{"org:*"}

// tokenTTL is long enough that no test can outlive its own token.
const tokenTTL = time.Hour

// Actor is an authenticated identity. Its Token is a genuine access token,
// minted through the OAuth server's own signing strategy, so requests carrying
// it run the same validation a production request runs.
type Actor struct {
	DID   syntax.DID
	Org   syntax.DID
	Token string
}

// NewOrg creates an org named name and returns its bootstrap admin.
func (p *TestPear) NewOrg(name string) *Actor {
	p.T.Helper()

	orgIdent, adminIdent, err := p.OrgStore.CreateOrg(
		p.T.Context(),
		name,
		"admin",
		"password",
		"password",
		fmt.Sprintf("%s-admin", name),
		name,
		fmt.Sprintf("admin@%s.test", name),
	)
	require.NoError(p.T, err)

	return p.ActorWithScopes(adminIdent.DID, orgIdent.DID, defaultScopes...)
}

// NewMember mints a member identity in org's organization and returns it. org
// must be an admin actor, since issuing the invite token requires one.
func (p *TestPear) NewMember(org *Actor, handle string) *Actor {
	p.T.Helper()

	token, err := p.OrgStore.IssueIdentityToken(
		p.T.Context(),
		org.Org,
		org.DID,
		false,
		time.Now().Add(time.Hour),
	)
	require.NoError(p.T, err)

	ident, err := p.OrgStore.CreateNewMemberIdentity(
		p.T.Context(),
		org.Org,
		token,
		handle,
		"password",
		fmt.Sprintf("%s@%s", handle, org.Org),
	)
	require.NoError(p.T, err)

	return p.ActorWithScopes(ident.DID, org.Org, defaultScopes...)
}

// Anonymous returns an actor with no credentials. Requests made as this actor
// carry no Authorization header.
func (p *TestPear) Anonymous() *Actor {
	return &Actor{}
}

// ActorWithScopes mints a token for an existing DID with exactly the scopes
// given. Use it to test that a handler rejects an under-scoped token.
func (p *TestPear) ActorWithScopes(did, org syntax.DID, scopes ...string) *Actor {
	p.T.Helper()

	token, err := p.OAuthServer.MintAccessToken(p.T.Context(), did, scopes, tokenTTL)
	require.NoError(p.T, err)

	return &Actor{DID: did, Org: org, Token: token}
}
