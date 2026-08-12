package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/stretchr/testify/require"
)

// fakeOAuthClient fakes the subset of *oauthclient.Client sessionResolver
// depends on. resumable tracks which (did, sessionID) pairs "exist"; minting
// a jwt-bearer session adds one under a fixed session ID and increments
// mintCalls, so tests can assert the grant is only exchanged once.
type fakeOAuthClient struct {
	resumable map[string]bool
	mintCalls int
}

func newFakeOAuthClient() *fakeOAuthClient {
	return &fakeOAuthClient{resumable: map[string]bool{}}
}

func resumeKey(did syntax.DID, sessionID string) string {
	return did.String() + "|" + sessionID
}

func (f *fakeOAuthClient) ResumeSession(
	_ context.Context,
	did syntax.DID,
	sessionID string,
) (*oauth.ClientSession, error) {
	if sessionID == "" || !f.resumable[resumeKey(did, sessionID)] {
		return nil, fmt.Errorf("no resumable session %q for %s", sessionID, did)
	}
	key, err := atcrypto.GeneratePrivateKeyP256()
	if err != nil {
		return nil, err
	}
	return &oauth.ClientSession{
		Data: &oauth.ClientSessionData{
			AccountDID:              did,
			SessionID:               sessionID,
			HostURL:                 "https://host.example",
			DPoPPrivateKeyMultibase: key.Multibase(),
		},
	}, nil
}

func (f *fakeOAuthClient) SendJWTTokenRequest(
	_ context.Context,
	identifier string,
) (*oauth.ClientSessionData, error) {
	f.mintCalls++
	sessionID := fmt.Sprintf("minted-%d", f.mintCalls)
	f.resumable[resumeKey(syntax.DID(identifier), sessionID)] = true
	return &oauth.ClientSessionData{AccountDID: syntax.DID(identifier), SessionID: sessionID}, nil
}

func TestSessionResolverClientForDIDResumesOAuthSession(t *testing.T) {
	t.Parallel()
	db := testutil.NewDB(t)
	client := newFakeOAuthClient()
	r, err := newSessionResolver(db, client)
	require.NoError(t, err)

	did := syntax.DID("did:web:member.example")
	client.resumable[resumeKey(did, "sess-1")] = true
	require.NoError(t, r.recordOAuthSession(t.Context(), did, "sess-1"))

	c, err := r.ClientForDID(t.Context(), did)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestSessionResolverClientForDIDMintsJWTBearerSessionOnFirstUse(t *testing.T) {
	t.Parallel()
	db := testutil.NewDB(t)
	client := newFakeOAuthClient()
	r, err := newSessionResolver(db, client)
	require.NoError(t, err)

	did := syntax.DID("did:web:member.example")
	require.NoError(t, r.recordJWTBearerSession(t.Context(), did))

	c, err := r.ClientForDID(t.Context(), did)
	require.NoError(t, err)
	require.NotNil(t, c)
	require.Equal(t, 1, client.mintCalls)

	var tracked trackedSession
	require.NoError(t, db.First(&tracked, "did = ?", did.String()).Error)
	require.Equal(t, "minted-1", tracked.SessionID)

	// A second call resumes the session minted above instead of re-minting.
	_, err = r.ClientForDID(t.Context(), did)
	require.NoError(t, err)
	require.Equal(t, 1, client.mintCalls)
}

func TestSessionResolverClientForDIDReMintsExpiredJWTBearerSession(t *testing.T) {
	t.Parallel()
	db := testutil.NewDB(t)
	client := newFakeOAuthClient()
	r, err := newSessionResolver(db, client)
	require.NoError(t, err)

	did := syntax.DID("did:web:member.example")
	require.NoError(t, r.recordJWTBearerSession(t.Context(), did))

	_, err = r.ClientForDID(t.Context(), did)
	require.NoError(t, err)
	require.Equal(t, 1, client.mintCalls)

	// Simulate the previously-minted session no longer resuming (e.g.
	// expired): it's no longer in the fake's resumable set.
	delete(client.resumable, resumeKey(did, "minted-1"))

	_, err = r.ClientForDID(t.Context(), did)
	require.NoError(t, err)
	require.Equal(t, 2, client.mintCalls)

	var tracked trackedSession
	require.NoError(t, db.First(&tracked, "did = ?", did.String()).Error)
	require.Equal(t, "minted-2", tracked.SessionID)
}

func TestSessionResolverClientForDIDUnknownDIDFails(t *testing.T) {
	t.Parallel()
	db := testutil.NewDB(t)
	r, err := newSessionResolver(db, newFakeOAuthClient())
	require.NoError(t, err)

	_, err = r.ClientForDID(t.Context(), syntax.DID("did:web:unknown.example"))
	require.Error(t, err)
}
