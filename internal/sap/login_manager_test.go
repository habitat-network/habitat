package sap

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/stretchr/testify/require"
)

func TestLoginManager_LoginFlowRoundTrip(t *testing.T) {
	db := testutil.NewDB(t)
	require.NoError(t, autoMigrate(db))

	m := newLoginManager(db)

	// A state with no saved flow is an org-admin bootstrap, not a user login.
	_, isUser, err := m.GetLoginFlow(t.Context(), "unknown-state")
	require.NoError(t, err)
	require.False(t, isUser)

	require.NoError(t, m.SaveLoginFlow(t.Context(), "state1", "https://docs.example/session/callback"))

	flow, isUser, err := m.GetLoginFlow(t.Context(), "state1")
	require.NoError(t, err)
	require.True(t, isUser)
	require.Equal(t, "https://docs.example/session/callback", flow.RedirectURL)

	// After the callback completes, the flow resolves to the authenticated DID.
	require.NoError(t, m.CompleteLoginFlow(t.Context(), "state1", "did:plc:alice"))
	did, err := m.GetCompletedLogin(t.Context(), "state1")
	require.NoError(t, err)
	require.Equal(t, syntax.DID("did:plc:alice"), did)
}

func TestStateFromAuthURL(t *testing.T) {
	state, err := stateFromAuthURL("https://pds.example/oauth/authorize?client_id=x&state=abc123&handle=alice")
	require.NoError(t, err)
	require.Equal(t, "abc123", state)

	_, err = stateFromAuthURL("https://pds.example/oauth/authorize?client_id=x")
	require.Error(t, err)
}
