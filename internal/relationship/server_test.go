package relationship

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/hive"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	testOrg = syntax.DID("did:plc:org")
	alice   = syntax.DID("did:plc:alice")
	bob     = syntax.DID("did:plc:bob")

	docsType  = syntax.NSID("network.habitat.docs")
	groupType = syntax.NSID("network.habitat.group")
)

// newTestServer returns a Server, along with the perms.Store and
// spaces.Store backing it, so tests can seed relations and spaces directly.
func newTestServer(t *testing.T, caller syntax.DID) (*Server, perms.Store, spaces.Store) {
	t.Helper()
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })

	db := db_testutil.NewDB(t)
	sp := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(db), spaces_testutil.WithFGA(fga))
	hve, err := hive.NewHive("example.com", "pear.example.com", db)
	require.NoError(t, err)
	blobStore := spaces_testutil.NewTestBlobStore(t)
	os, err := opensocial.NewStore(db, sp, blobStore, hve)
	require.NoError(t, err)
	ps := perms.NewStore(db, sp, fga, os)

	return NewServer(
		ps,
		sp,
		authntest.NewSuccessValidatorWithOrg(caller, caller),
	), ps, sp
}

// newSpace creates a space owned by org in sp and returns its URI.
func newSpace(
	t *testing.T,
	sp spaces.Store,
	spaceType syntax.NSID,
	skey string,
) habitat_syntax.SpaceURI {
	t.Helper()
	uri, err := sp.CreateSpace(
		t.Context(),
		testOrg,
		spaceType,
		habitat_syntax.SpaceKey(skey),
	)
	require.NoError(t, err)
	return uri
}

func TestServer_SetUserRelation(t *testing.T) {
	// caller is the org (space owner), so it has the manager role implicitly.
	s, ps, sp := newTestServer(t, testOrg)
	space := newSpace(t, sp, docsType, "doc")

	var out habitat.NetworkHabitatRelationshipSetUserRelationOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.SetUserRelation,
		habitat.NetworkHabitatRelationshipSetUserRelationInput{
			Subject: alice.String(), Relation: "reader", Space: space.String(),
		},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, out.Uri)

	allowed, err := ps.CheckUserHasSpaceRole(
		t.Context(), alice, space, habitat_syntax.SpaceRoleReader,
	)
	require.NoError(t, err)
	require.True(t, allowed)
}

func TestServer_SetUserRelation_BadBody(t *testing.T) {
	// A malformed JSON body can't go through TestXRPCClient, which marshals
	// its input and would fail before the request is ever sent.
	s, _, _ := newTestServer(t, testOrg)
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.relationship.setUserRelation",
		strings.NewReader("{not json"),
	)
	w := httptest.NewRecorder()
	s.SetUserRelation(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_SetUserRelation_BadSpace(t *testing.T) {
	s, _, _ := newTestServer(t, testOrg)

	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.SetUserRelation,
		habitat.NetworkHabitatRelationshipSetUserRelationInput{
			Subject: alice.String(), Relation: "reader", Space: "not-a-space",
		},
		&out,
	)
	require.Equal(t, http.StatusBadRequest, code)
}

func TestServer_SetUserRelation_InvalidRelation(t *testing.T) {
	s, _, sp := newTestServer(t, testOrg)
	space := newSpace(t, sp, docsType, "doc")

	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.SetUserRelation,
		habitat.NetworkHabitatRelationshipSetUserRelationInput{
			Subject: alice.String(), Relation: "bogus", Space: space.String(),
		},
		&out,
	)
	require.Equal(t, http.StatusBadRequest, code)
}

func TestServer_SetSpaceRelation(t *testing.T) {
	s, ps, sp := newTestServer(t, testOrg)
	group := newSpace(t, sp, groupType, "team")
	space := newSpace(t, sp, docsType, "doc")

	var out habitat.NetworkHabitatRelationshipSetSpaceRelationOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.SetSpaceRelation,
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

	allowed, err := ps.CheckSpaceRelationHasSpaceRole(
		t.Context(),
		group, habitat_syntax.SpaceRoleReader,
		space, habitat_syntax.SpaceRoleWriter,
	)
	require.NoError(t, err)
	require.True(t, allowed)
}

func TestServer_SetSpaceRelation_InvalidSubjectRole(t *testing.T) {
	s, _, sp := newTestServer(t, testOrg)
	group := newSpace(t, sp, groupType, "team")
	space := newSpace(t, sp, docsType, "doc")

	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.SetSpaceRelation,
		habitat.NetworkHabitatRelationshipSetSpaceRelationInput{
			Subject: group.String(), SubjectRole: "bogus", Relation: "writer", Space: space.String(),
		},
		&out,
	)
	require.Equal(t, http.StatusBadRequest, code)
}

func TestServer_DeleteRelation_BadURI(t *testing.T) {
	s, _, _ := newTestServer(t, testOrg)

	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.DeleteRelation,
		habitat.NetworkHabitatRelationshipDeleteRelationInput{Uri: "not-a-uri"},
		&out,
	)
	require.Equal(t, http.StatusBadRequest, code)
}

func TestServer_ListRelations(t *testing.T) {
	s, ps, sp := newTestServer(t, testOrg)
	group := newSpace(t, sp, groupType, "team")
	space := newSpace(t, sp, docsType, "doc")
	_, err := ps.SetUserRelation(t.Context(), alice, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)
	_, err = ps.SetUserRelation(t.Context(), bob, space, habitat_syntax.SpaceRoleWriter)
	require.NoError(t, err)
	_, err = ps.SetSpaceRoleRelation(
		t.Context(),
		group, habitat_syntax.SpaceRoleReader,
		space, habitat_syntax.SpaceRoleReader,
	)
	require.NoError(t, err)

	client := httpx_testutil.NewTestXRPCClient(t)

	var out habitat.NetworkHabitatRelationshipListRelationsOutput
	code := client.Query(
		s.ListRelations,
		url.Values{"space": {space.String()}},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, out.Relations, 3)

	t.Run("filters by subjectType", func(t *testing.T) {
		var out habitat.NetworkHabitatRelationshipListRelationsOutput
		code := client.Query(
			s.ListRelations,
			url.Values{"space": {space.String()}, "subjectType": {"user"}},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
		require.Len(t, out.Relations, 2)
	})

	t.Run("filters by relation", func(t *testing.T) {
		var out habitat.NetworkHabitatRelationshipListRelationsOutput
		code := client.Query(
			s.ListRelations,
			url.Values{"space": {space.String()}, "relation": {"writer"}},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
		require.Len(t, out.Relations, 1)
	})

	t.Run("filters by subjectDid", func(t *testing.T) {
		var out habitat.NetworkHabitatRelationshipListRelationsOutput
		code := client.Query(
			s.ListRelations,
			url.Values{"space": {space.String()}, "subjectDid": {alice.String()}},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
		require.Len(t, out.Relations, 1)
	})
}

func TestServer_ListRelations_InvalidSubjectType(t *testing.T) {
	s, _, sp := newTestServer(t, testOrg)
	space := newSpace(t, sp, docsType, "doc")

	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.ListRelations,
		url.Values{"space": {space.String()}, "subjectType": {"clique"}},
		&out,
	)
	require.Equal(t, http.StatusBadRequest, code)
}

func TestServer_CheckUserRelation(t *testing.T) {
	s, ps, sp := newTestServer(t, testOrg)
	space := newSpace(t, sp, docsType, "doc")
	_, err := ps.SetUserRelation(t.Context(), alice, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)

	var out habitat.NetworkHabitatRelationshipCheckUserRelationOutput
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.CheckUserRelation,
		url.Values{"space": {space.String()}, "subject": {alice.String()}, "relation": {"reader"}},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	require.True(t, out.Allowed)
}

func TestServer_CheckUserRelation_InvalidRelation(t *testing.T) {
	s, _, sp := newTestServer(t, testOrg)
	space := newSpace(t, sp, docsType, "doc")

	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.CheckUserRelation,
		url.Values{"space": {space.String()}, "subject": {alice.String()}, "relation": {"bogus"}},
		&out,
	)
	require.Equal(t, http.StatusBadRequest, code)
}

func TestServer_CheckSpaceRelation(t *testing.T) {
	s, ps, sp := newTestServer(t, testOrg)
	group := newSpace(t, sp, groupType, "team")
	space := newSpace(t, sp, docsType, "doc")
	_, err := ps.SetSpaceRoleRelation(
		t.Context(),
		group, habitat_syntax.SpaceRoleReader,
		space, habitat_syntax.SpaceRoleWriter,
	)
	require.NoError(t, err)

	var out habitat.NetworkHabitatRelationshipCheckSpaceRelationOutput
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.CheckSpaceRelation,
		url.Values{
			"space": {space.String()}, "subject": {group.String()},
			"subjectRole": {"reader"}, "relation": {"writer"},
		},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	require.True(t, out.Allowed)
}

func TestServer_ResolveRelations(t *testing.T) {
	s, ps, sp := newTestServer(t, testOrg)
	space := newSpace(t, sp, docsType, "doc")
	_, err := ps.SetUserRelation(t.Context(), alice, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)

	var out habitat.NetworkHabitatRelationshipResolveRelationsOutput
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.ResolveRelations,
		url.Values{"space": {space.String()}, "relation": {"reader"}},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, out.Dids, alice.String())
}

func TestServer_ListRelatedSpaces(t *testing.T) {
	s, ps, sp := newTestServer(t, testOrg)
	space := newSpace(t, sp, docsType, "doc")
	_, err := ps.SetUserRelation(t.Context(), alice, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)

	var out habitat.NetworkHabitatRelationshipListRelatedSpacesOutput
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.ListRelatedSpaces,
		url.Values{"did": {alice.String()}, "relation": {"reader"}},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, out.Spaces, space.String())
}

func TestServer_ListRelatedSpaces_FiltersUnreadable(t *testing.T) {
	// bob can see his own access via the DID param but only spaces bob can read
	// are returned. bob is a reader of the space, so it is returned.
	s, ps, sp := newTestServer(t, bob)
	space := newSpace(t, sp, docsType, "doc")
	_, err := ps.SetUserRelation(t.Context(), bob, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)

	var out habitat.NetworkHabitatRelationshipListRelatedSpacesOutput
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.ListRelatedSpaces,
		url.Values{"did": {bob.String()}, "relation": {"reader"}},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, out.Spaces, space.String())
}

func TestServer_Unauthenticated(t *testing.T) {
	_, ps, sp := newTestServer(t, testOrg)
	space := newSpace(t, sp, docsType, "doc")
	s := NewServer(ps, sp, authntest.NewFailureValidator())

	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.CheckUserRelation,
		url.Values{"space": {space.String()}, "subject": {alice.String()}, "relation": {"reader"}},
		&out,
	)
	require.Equal(t, http.StatusUnauthorized, code)
}

func Test_authorizeCanWrite(t *testing.T) {
	s, ps, sp := newTestServer(t, testOrg)
	space := newSpace(t, sp, docsType, "doc")

	_, err := ps.SetUserRelation(t.Context(), alice, space, habitat_syntax.SpaceRoleManager)
	require.NoError(t, err)

	t.Run("caller is not space owner but subject is", func(t *testing.T) {
		require.False(t, s.authorizeCanWrite(
			t.Context(),
			httptest.NewRecorder(),
			&authn.CredentialInfo{Subject: alice},
			true,
			space,
			habitat_syntax.SpaceRoleReader,
		))
	})
	t.Run("caller is not space owner but role is", func(t *testing.T) {
		require.False(t, s.authorizeCanWrite(
			t.Context(),
			httptest.NewRecorder(),
			&authn.CredentialInfo{Subject: alice},
			false,
			space,
			habitat_syntax.SpaceRoleOwner,
		))
	})
	t.Run("caller is space owner but subject isn't", func(t *testing.T) {
		require.True(t, s.authorizeCanWrite(
			t.Context(),
			httptest.NewRecorder(),
			&authn.CredentialInfo{Subject: testOrg},
			false,
			space,
			habitat_syntax.SpaceRoleOwner,
		))
	})
	t.Run("caller and subject are space owner", func(t *testing.T) {
		require.True(t, s.authorizeCanWrite(
			t.Context(),
			httptest.NewRecorder(),
			&authn.CredentialInfo{Subject: testOrg},
			true,
			space,
			habitat_syntax.SpaceRoleReader,
		))
	})
	t.Run("caller and subject are not space owner", func(t *testing.T) {
		require.True(t, s.authorizeCanWrite(
			t.Context(),
			httptest.NewRecorder(),
			&authn.CredentialInfo{Subject: alice},
			false,
			space,
			habitat_syntax.SpaceRoleReader,
		))
	})
}
