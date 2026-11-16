package pearserver_test

import (
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/comatproto"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
)

func TestServer_PutAndRemoveMembers(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	client := httpx_testutil.NewTestXRPCClient(t)
	uri, err := ts.SimpleStore.CreateSpace(t.Context(), owner, groupTp, "batch")
	require.NoError(t, err)

	isMember := func(did string) bool {
		t.Helper()
		ok, err := ts.SimpleStore.IsMember(t.Context(), org, uri, syntax.DID(did))
		require.NoError(t, err)
		return ok
	}

	var none struct{}
	code := client.Procedure(ts.Server.PutMembers, comatproto.ComAtprotoMemberspacePutMembersInput{
		Space: uri.String(),
		Members: []comatproto.ComAtprotoMemberspaceDefsMember{
			{Did: "did:plc:alice", Read: true, Write: true},
			{Did: "did:plc:bob", Read: true, Write: true},
		},
	}, &none)
	require.Equal(t, http.StatusOK, code)
	require.True(t, isMember("did:plc:alice"))
	require.True(t, isMember("did:plc:bob"))

	// Per-member read/write is not supported, so a partial grant is refused
	// before anything is applied.
	var apiErr atclient.ErrorBody
	code = client.Procedure(ts.Server.PutMembers, comatproto.ComAtprotoMemberspacePutMembersInput{
		Space: uri.String(),
		Members: []comatproto.ComAtprotoMemberspaceDefsMember{
			{Did: "did:plc:carol", Read: true, Write: true},
			{Did: "did:plc:dave", Read: true, Write: false},
		},
	}, &apiErr)
	require.Equal(t, http.StatusBadRequest, code)
	require.False(t, isMember("did:plc:carol"))

	// Non-members in the list are ignored.
	code = client.Procedure(ts.Server.RemoveMembers, comatproto.ComAtprotoMemberspaceRemoveMembersInput{
		Space: uri.String(),
		Dids:  []string{"did:plc:alice", "did:plc:stranger"},
	}, &none)
	require.Equal(t, http.StatusOK, code)
	require.False(t, isMember("did:plc:alice"))
	require.True(t, isMember("did:plc:bob"))
}

func TestServer_PutMembersRequiresMemberspacePolicy(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	uri, err := ts.SimpleStore.CreateSpace(t.Context(), owner, groupTp, "personal")
	require.NoError(t, err)
	require.NoError(t, ts.SpaceStore.SetSpacePolicy(
		t.Context(), uri, spaces.Policy{Type: spaces.PolicyPersonalSpace},
	))

	var apiErr atclient.ErrorBody
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.PutMembers,
		comatproto.ComAtprotoMemberspacePutMembersInput{
			Space:   uri.String(),
			Members: []comatproto.ComAtprotoMemberspaceDefsMember{{Did: "did:plc:alice", Read: true, Write: true}},
		},
		&apiErr,
	)
	require.Equal(t, http.StatusBadRequest, code)
	require.Equal(t, "PolicyMismatch", apiErr.Name)
}
