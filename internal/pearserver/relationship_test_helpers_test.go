package pearserver_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// newRelationshipServer returns a TestServer authenticated as caller, along
// with the perms.Store and spaces.Store backing it, so tests can seed
// relations and spaces directly.
func newRelationshipServer(
	t *testing.T,
	caller syntax.DID,
) *pearserver_testutil.TestServer {
	t.Helper()
	ts := pearserver_testutil.NewTestServer(t,
		pearserver_testutil.WithValidator(
			authntest.NewSuccessValidatorWithOrg(caller, org),
		),
	)
	return ts
}

// newSpace creates a space owned by org in sp and returns its URI.
func newSpace(
	t *testing.T,
	ts *pearserver_testutil.TestServer,
	spaceType syntax.NSID,
	skey string,
) habitat_syntax.SpaceURI {
	t.Helper()
	uri, err := ts.SpaceStore.CreateSpace(
		t.Context(),
		org,
		spaceType,
		habitat_syntax.SpaceKey(skey),
	)
	require.NoError(t, err)
	return uri
}
