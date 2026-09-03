package pearserver_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// TestServer_Relationship covers the network.habitat.relationship.* endpoints
// with a single shared TestServer (caller = org, the space owner), giving each
// subtest its own distinctly-keyed space so they don't interfere on the shared
// store.
func TestServer_Relationship(t *testing.T) {
	ts := newRelationshipServer(t, org)
	client := httpx_testutil.NewTestXRPCClient(t)

	// newSpace creates a space owned by org with a distinct key per subtest.
	newSpace := func(t *testing.T, spaceType syntax.NSID, skey string) habitat_syntax.SpaceURI {
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

	t.Run("set user relation", func(t *testing.T) {
		t.Run("creates a reader relation", func(t *testing.T) {
			space := newSpace(t, docsTp, "sur-reader")
			var out habitat.NetworkHabitatRelationshipSetUserRelationOutput
			code := client.Procedure(
				ts.Server.SetUserRelation,
				habitat.NetworkHabitatRelationshipSetUserRelationInput{
					Subject: alice.String(), Relation: "reader", Space: space.String(),
				},
				&out,
			)
			require.Equal(t, http.StatusOK, code)
			require.NotEmpty(t, out.Uri)

			allowed, err := ts.PermStore.CheckUserHasSpaceRole(
				t.Context(), alice, space, habitat_syntax.SpaceRoleReader,
			)
			require.NoError(t, err)
			require.True(t, allowed)
		})

		t.Run("rejects a malformed body", func(t *testing.T) {
			// A malformed JSON body can't go through TestXRPCClient, which
			// marshals its input and would fail before the request is sent.
			req := httptest.NewRequest(
				http.MethodPost,
				"/xrpc/network.habitat.relationship.setUserRelation",
				strings.NewReader("{not json"),
			)
			w := httptest.NewRecorder()
			ts.Server.SetUserRelation(w, req)
			require.Equal(t, http.StatusBadRequest, w.Code)
		})

		t.Run("rejects an invalid space", func(t *testing.T) {
			var out struct{}
			code := client.Procedure(
				ts.Server.SetUserRelation,
				habitat.NetworkHabitatRelationshipSetUserRelationInput{
					Subject: alice.String(), Relation: "reader", Space: "not-a-space",
				},
				&out,
			)
			require.Equal(t, http.StatusBadRequest, code)
		})

		t.Run("rejects an invalid relation", func(t *testing.T) {
			space := newSpace(t, docsTp, "sur-invalid")
			var out struct{}
			code := client.Procedure(
				ts.Server.SetUserRelation,
				habitat.NetworkHabitatRelationshipSetUserRelationInput{
					Subject: alice.String(), Relation: "bogus", Space: space.String(),
				},
				&out,
			)
			require.Equal(t, http.StatusBadRequest, code)
		})
	})

	t.Run("set space relation", func(t *testing.T) {
		t.Run("creates a space relation", func(t *testing.T) {
			group := newSpace(t, groupTp, "ssr-team")
			space := newSpace(t, docsTp, "ssr-doc")
			var out habitat.NetworkHabitatRelationshipSetSpaceRelationOutput
			code := client.Procedure(
				ts.Server.SetSpaceRelation,
				habitat.NetworkHabitatRelationshipSetSpaceRelationInput{
					Subject:     group.String(),
					SubjectRole: "reader",
					Relation:    "writer",
					Space:       space.String(),
				},
				&out,
			)
			require.Equal(t, http.StatusOK, code)
			require.NotEmpty(t, out.Uri)

			allowed, err := ts.PermStore.CheckSpaceRelationHasSpaceRole(
				t.Context(),
				group, habitat_syntax.SpaceRoleReader,
				space, habitat_syntax.SpaceRoleWriter,
			)
			require.NoError(t, err)
			require.True(t, allowed)
		})

		t.Run("rejects an invalid subject role", func(t *testing.T) {
			group := newSpace(t, groupTp, "ssr-badteam")
			space := newSpace(t, docsTp, "ssr-baddoc")
			var out struct{}
			code := client.Procedure(
				ts.Server.SetSpaceRelation,
				habitat.NetworkHabitatRelationshipSetSpaceRelationInput{
					Subject:     group.String(),
					SubjectRole: "bogus",
					Relation:    "writer",
					Space:       space.String(),
				},
				&out,
			)
			require.Equal(t, http.StatusBadRequest, code)
		})
	})

	t.Run("delete relation", func(t *testing.T) {
		t.Run("rejects a bad uri", func(t *testing.T) {
			var out struct{}
			code := client.Procedure(
				ts.Server.DeleteRelation,
				habitat.NetworkHabitatRelationshipDeleteRelationInput{Uri: "not-a-uri"},
				&out,
			)
			require.Equal(t, http.StatusBadRequest, code)
		})
	})

	t.Run("list relations", func(t *testing.T) {
		group := newSpace(t, groupTp, "lr-team")
		space := newSpace(t, docsTp, "lr-doc")
		_, err := ts.PermStore.SetUserRelation(
			t.Context(),
			alice,
			space,
			habitat_syntax.SpaceRoleReader,
		)
		require.NoError(t, err)
		_, err = ts.PermStore.SetUserRelation(
			t.Context(),
			bob,
			space,
			habitat_syntax.SpaceRoleWriter,
		)
		require.NoError(t, err)
		_, err = ts.PermStore.SetSpaceRoleRelation(
			t.Context(),
			group, habitat_syntax.SpaceRoleReader,
			space, habitat_syntax.SpaceRoleReader,
		)
		require.NoError(t, err)

		t.Run("lists all relations", func(t *testing.T) {
			var out habitat.NetworkHabitatRelationshipListRelationsOutput
			code := client.Query(
				ts.Server.ListRelations,
				url.Values{"space": {space.String()}},
				&out,
			)
			require.Equal(t, http.StatusOK, code)
			require.Len(t, out.Relations, 3)
		})

		t.Run("filters by subjectType", func(t *testing.T) {
			var out habitat.NetworkHabitatRelationshipListRelationsOutput
			code := client.Query(
				ts.Server.ListRelations,
				url.Values{"space": {space.String()}, "subjectType": {"user"}},
				&out,
			)
			require.Equal(t, http.StatusOK, code)
			require.Len(t, out.Relations, 2)
		})

		t.Run("filters by relation", func(t *testing.T) {
			var out habitat.NetworkHabitatRelationshipListRelationsOutput
			code := client.Query(
				ts.Server.ListRelations,
				url.Values{"space": {space.String()}, "relation": {"writer"}},
				&out,
			)
			require.Equal(t, http.StatusOK, code)
			require.Len(t, out.Relations, 1)
		})

		t.Run("filters by subjectDid", func(t *testing.T) {
			var out habitat.NetworkHabitatRelationshipListRelationsOutput
			code := client.Query(
				ts.Server.ListRelations,
				url.Values{"space": {space.String()}, "subjectDid": {alice.String()}},
				&out,
			)
			require.Equal(t, http.StatusOK, code)
			require.Len(t, out.Relations, 1)
		})

		t.Run("rejects an invalid subjectType", func(t *testing.T) {
			var out struct{}
			code := client.Query(
				ts.Server.ListRelations,
				url.Values{"space": {space.String()}, "subjectType": {"clique"}},
				&out,
			)
			require.Equal(t, http.StatusBadRequest, code)
		})
	})

	t.Run("check user relation", func(t *testing.T) {
		space := newSpace(t, docsTp, "cur-doc")
		_, err := ts.PermStore.SetUserRelation(
			t.Context(),
			alice,
			space,
			habitat_syntax.SpaceRoleReader,
		)
		require.NoError(t, err)

		t.Run("allows a granted reader", func(t *testing.T) {
			var out habitat.NetworkHabitatRelationshipCheckUserRelationOutput
			code := client.Query(
				ts.Server.CheckUserRelation,
				url.Values{
					"space":    {space.String()},
					"subject":  {alice.String()},
					"relation": {"reader"},
				},
				&out,
			)
			require.Equal(t, http.StatusOK, code)
			require.True(t, out.Allowed)
		})

		t.Run("rejects an invalid relation", func(t *testing.T) {
			var out struct{}
			code := client.Query(
				ts.Server.CheckUserRelation,
				url.Values{
					"space":    {space.String()},
					"subject":  {alice.String()},
					"relation": {"bogus"},
				},
				&out,
			)
			require.Equal(t, http.StatusBadRequest, code)
		})
	})

	t.Run("check space relation", func(t *testing.T) {
		group := newSpace(t, groupTp, "csr-team")
		space := newSpace(t, docsTp, "csr-doc")
		_, err := ts.PermStore.SetSpaceRoleRelation(
			t.Context(),
			group, habitat_syntax.SpaceRoleReader,
			space, habitat_syntax.SpaceRoleWriter,
		)
		require.NoError(t, err)

		var out habitat.NetworkHabitatRelationshipCheckSpaceRelationOutput
		code := client.Query(
			ts.Server.CheckSpaceRelation,
			url.Values{
				"space": {space.String()}, "subject": {group.String()},
				"subjectRole": {"reader"}, "relation": {"writer"},
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
		require.True(t, out.Allowed)
	})

	t.Run("resolve relations", func(t *testing.T) {
		space := newSpace(t, docsTp, "rr-doc")
		_, err := ts.PermStore.SetUserRelation(
			t.Context(),
			alice,
			space,
			habitat_syntax.SpaceRoleReader,
		)
		require.NoError(t, err)

		var out habitat.NetworkHabitatRelationshipResolveRelationsOutput
		code := client.Query(
			ts.Server.ResolveRelations,
			url.Values{"space": {space.String()}, "relation": {"reader"}},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
		require.Contains(t, out.Dids, alice.String())
	})

	t.Run("list related spaces", func(t *testing.T) {
		space := newSpace(t, docsTp, "lrs-doc")
		_, err := ts.PermStore.SetUserRelation(
			t.Context(),
			alice,
			space,
			habitat_syntax.SpaceRoleReader,
		)
		require.NoError(t, err)

		var out habitat.NetworkHabitatRelationshipListRelatedSpacesOutput
		code := client.Query(
			ts.Server.ListRelatedSpaces,
			url.Values{"did": {alice.String()}, "relation": {"reader"}},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
		require.Contains(t, out.Spaces, space.String())
	})
}

// TestServer_ListRelatedSpaces_FiltersUnreadable uses bob as the caller, so it
// needs its own server.
func TestServer_ListRelatedSpaces_FiltersUnreadable(t *testing.T) {
	// bob can see his own access via the DID param but only spaces bob can read
	// are returned. bob is a reader of the space, so it is returned.
	ts := newRelationshipServer(t, bob)
	space := newSpace(t, ts, docsTp, "lrsb-doc")
	_, err := ts.PermStore.SetUserRelation(t.Context(), bob, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)

	var out habitat.NetworkHabitatRelationshipListRelatedSpacesOutput
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		ts.Server.ListRelatedSpaces,
		url.Values{"did": {bob.String()}, "relation": {"reader"}},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, out.Spaces, space.String())
}

func TestServer_Unauthenticated(t *testing.T) {
	ts := newRelationshipServer(t, org)
	space := newSpace(t, ts, docsTp, "unauth-doc")
	failTS := pearserver_testutil.NewTestServer(t,
		pearserver_testutil.WithValidator(authntest.NewFailureValidator()),
	)

	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		failTS.Server.CheckUserRelation,
		url.Values{"space": {space.String()}, "subject": {alice.String()}, "relation": {"reader"}},
		&out,
	)
	require.Equal(t, http.StatusUnauthorized, code)
}

// TestServer_SetUserRelation_authorizeCanWrite pins the authorization logic
// in authorizeCanWrite (previously exercised directly via the unexported
// helper) through the public SetUserRelation handler.
func TestServer_SetUserRelation_authorizeCanWrite(t *testing.T) {
	t.Run("caller is not space owner but subject is", func(t *testing.T) {
		ts := newRelationshipServer(t, alice)
		space := newSpace(t, ts, docsTp, "awc-org")

		var out struct{}
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			ts.Server.SetUserRelation,
			habitat.NetworkHabitatRelationshipSetUserRelationInput{
				Subject: org.String(), Relation: "reader", Space: space.String(),
			},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})
	t.Run("caller is not space owner but role is", func(t *testing.T) {
		ts := newRelationshipServer(t, alice)
		space := newSpace(t, ts, docsTp, "awc-role")

		var out struct{}
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			ts.Server.SetUserRelation,
			habitat.NetworkHabitatRelationshipSetUserRelationInput{
				Subject: alice.String(), Relation: "owner", Space: space.String(),
			},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})
	t.Run("caller is space owner but subject isn't", func(t *testing.T) {
		ts := newRelationshipServer(t, org)
		space := newSpace(t, ts, docsTp, "awc-subject")

		var out habitat.NetworkHabitatRelationshipSetUserRelationOutput
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			ts.Server.SetUserRelation,
			habitat.NetworkHabitatRelationshipSetUserRelationInput{
				Subject: alice.String(), Relation: "owner", Space: space.String(),
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
	})
	t.Run("caller and subject are space owner", func(t *testing.T) {
		ts := newRelationshipServer(t, org)
		space := newSpace(t, ts, docsTp, "awc-owner")

		var out habitat.NetworkHabitatRelationshipSetUserRelationOutput
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			ts.Server.SetUserRelation,
			habitat.NetworkHabitatRelationshipSetUserRelationInput{
				Subject: org.String(), Relation: "reader", Space: space.String(),
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
	})
	t.Run("caller and subject are not space owner", func(t *testing.T) {
		ts := newRelationshipServer(t, alice)
		space := newSpace(t, ts, docsTp, "awc-nonowner")

		var out habitat.NetworkHabitatRelationshipSetUserRelationOutput
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			ts.Server.SetUserRelation,
			habitat.NetworkHabitatRelationshipSetUserRelationInput{
				Subject: alice.String(), Relation: "reader", Space: space.String(),
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
	})
}
