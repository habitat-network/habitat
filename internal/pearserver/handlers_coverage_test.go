package pearserver_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestServer_UploadImage(t *testing.T) {
	ts := newOpenSocialServer(t, admin)
	orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)

	upload := func(server *pearserver_testutil.TestServer, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(
			http.MethodPost,
			"/xrpc/community.opensocial.uploadImage?org="+url.QueryEscape(orgDID),
			bytes.NewReader(body),
		)
		req.Header.Set("Content-Type", "image/png")
		w := httptest.NewRecorder()
		server.Server.UploadImage(w, req)
		return w
	}

	t.Run("admin uploads an image", func(t *testing.T) {
		w := upload(ts, []byte("not really a png"))
		require.Equal(t, http.StatusOK, w.Code)

		var out opensocial_api.CommunityOpensocialUploadImageOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		require.Equal(t, "image/png", out.Blob.MimeType)
	})

	t.Run("non-admin cannot upload", func(t *testing.T) {
		aliceTS := newOpenSocialServer(t, alice)
		w := upload(aliceTS, []byte("image"))
		require.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("rejects an oversized image", func(t *testing.T) {
		w := upload(ts, make([]byte, 500*1024+1))
		require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	})

	t.Run("rejects an invalid org", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodPost,
			"/xrpc/community.opensocial.uploadImage?org=not-a-did",
			strings.NewReader("image"),
		)
		w := httptest.NewRecorder()
		ts.Server.UploadImage(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestServer_GetDelegationToken(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)
	ts := newRelationshipServer(t, owner)
	space := newSpace(t, ts, docsTp, "delegation")

	var out habitat.NetworkHabitatSpaceGetDelegationTokenOutput
	code := client.Query(
		ts.Server.GetDelegationToken,
		url.Values{"space": {space.String()}},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, out.Token)

	code = client.Query(
		ts.Server.GetDelegationToken,
		url.Values{"space": {"not-a-space"}},
		&struct{}{},
	)
	require.Equal(t, http.StatusBadRequest, code)
}

func TestServer_McpDisconnectServer(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)
	ts := newOpenSocialServer(t, admin)
	orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)

	var addOut habitat.NetworkHabitatMcpAddServerOutput
	require.Equal(t, http.StatusOK, client.Procedure(
		ts.Server.AddServer,
		habitat.NetworkHabitatMcpAddServerInput{
			Org: orgDID, Name: "Linear", Url: "https://mcp.example.com/mcp", AuthType: "oauth",
		},
		&addOut,
	))

	t.Run("disconnecting a server that isn't connected is not found", func(t *testing.T) {
		var out struct{}
		code := client.Procedure(
			ts.Server.DisconnectServer,
			habitat.NetworkHabitatMcpDisconnectServerInput{Org: orgDID, Id: addOut.Server.Id},
			&out,
		)
		require.Equal(t, http.StatusNotFound, code)
	})

	t.Run("disconnects a connected server", func(t *testing.T) {
		ts.NangoClient.Connect(
			"conn-admin", nangoKey(orgDID, addOut.Server.Id), admin.String(),
		)
		var out struct{}
		code := client.Procedure(
			ts.Server.DisconnectServer,
			habitat.NetworkHabitatMcpDisconnectServerInput{Org: orgDID, Id: addOut.Server.Id},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
	})

	t.Run("requires an id", func(t *testing.T) {
		var out struct{}
		code := client.Procedure(
			ts.Server.DisconnectServer,
			habitat.NetworkHabitatMcpDisconnectServerInput{Org: orgDID},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})
}

func TestServer_GetBlobErrors(t *testing.T) {
	ts := newRelationshipServer(t, owner)
	space := newSpace(t, ts, docsTp, "blob-errors")
	otherSpace := newSpace(t, ts, docsTp, "blob-errors-other")
	client := httpx_testutil.NewTestXRPCClient(t)

	upReq := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("blob"))
	upW := httptest.NewRecorder()
	ts.Server.UploadBlob(upW, upReq)
	require.Equal(t, http.StatusOK, upW.Code)
	var uploaded habitat.NetworkHabitatRepoUploadBlobOutput
	require.NoError(t, json.NewDecoder(upW.Body).Decode(&uploaded))

	t.Run("rejects a malformed cid", func(t *testing.T) {
		var out struct{}
		code := client.Query(
			ts.Server.GetBlob,
			url.Values{"space": {space.String()}, "cid": {"not-a-cid"}},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("rejects a blob the space does not reference", func(t *testing.T) {
		var out struct{}
		code := client.Query(
			ts.Server.GetBlob,
			url.Values{"space": {otherSpace.String()}, "cid": {uploaded.Cid}},
			&out,
		)
		require.Equal(t, http.StatusNotFound, code)
	})
}

func TestServer_GetRecordErrors(t *testing.T) {
	ts := newRelationshipServer(t, owner)
	space := newSpace(t, ts, docsTp, "record-errors")
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("rejects a malformed rkey", func(t *testing.T) {
		var out struct{}
		code := client.Query(
			ts.Server.GetRecord,
			url.Values{
				"space":      {space.String()},
				"repo":       {org.String()},
				"collection": {docsTp.String()},
				"rkey":       {"not a rkey!"},
			},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("missing record is not found", func(t *testing.T) {
		var out struct{}
		code := client.Query(
			ts.Server.GetRecord,
			url.Values{
				"space":      {space.String()},
				"repo":       {org.String()},
				"collection": {docsTp.String()},
				"rkey":       {"missing"},
			},
			&out,
		)
		require.Equal(t, http.StatusNotFound, code)
	})
}

func TestServer_RelationshipErrors(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)
	ts := newRelationshipServer(t, org)
	space := newSpace(t, ts, docsTp, "relationship-errors")

	t.Run("check space relation rejects an invalid relation", func(t *testing.T) {
		var out struct{}
		code := client.Query(
			ts.Server.CheckSpaceRelation,
			url.Values{
				"space": {space.String()}, "subject": {space.String()},
				"subjectRole": {"reader"}, "relation": {"bogus"},
			},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("resolve spaces rejects an invalid relation", func(t *testing.T) {
		var out struct{}
		code := client.Query(
			ts.Server.ResolveSpaces,
			url.Values{"space": {space.String()}, "relation": {"bogus"}},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("list related spaces rejects an invalid relation", func(t *testing.T) {
		var out struct{}
		code := client.Query(
			ts.Server.ListRelatedSpaces,
			url.Values{"did": {org.String()}, "relation": {"bogus"}},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})
}

func TestServer_DeleteRelationSuccess(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)
	ts := newRelationshipServer(t, org)
	space := newSpace(t, ts, docsTp, "delete-relation")

	uri, err := ts.PermStore.SetUserRelation(
		t.Context(), alice, space, habitat_syntax.SpaceRoleReader,
	)
	require.NoError(t, err)

	t.Run("deletes an existing relation", func(t *testing.T) {
		var out struct{}
		code := client.Procedure(
			ts.Server.DeleteRelation,
			habitat.NetworkHabitatRelationshipDeleteRelationInput{Uri: uri.String()},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
	})

	t.Run("missing relation is not found", func(t *testing.T) {
		var out struct{}
		code := client.Procedure(
			ts.Server.DeleteRelation,
			habitat.NetworkHabitatRelationshipDeleteRelationInput{Uri: uri.String()},
			&out,
		)
		require.Equal(t, http.StatusNotFound, code)
	})

	t.Run("rejects a malformed body", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodPost,
			"/xrpc/network.habitat.relationship.deleteRelation",
			strings.NewReader("{not json"),
		)
		w := httptest.NewRecorder()
		ts.Server.DeleteRelation(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestServer_HandlerValidationErrors(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("mcp update requires an id", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		var out struct{}
		code := client.Procedure(
			ts.Server.UpdateServer,
			habitat.NetworkHabitatMcpUpdateServerInput{Org: orgDID},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("mcp remove requires an id", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		var out struct{}
		code := client.Procedure(
			ts.Server.RemoveServer,
			habitat.NetworkHabitatMcpRemoveServerInput{Org: orgDID},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("llm update requires an id", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		var out struct{}
		code := client.Procedure(
			ts.Server.UpdateLlmConnection,
			habitat.NetworkHabitatLlmUpdateConnectionInput{Org: orgDID},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("create invite rejects an invalid invitee", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		var out struct{}
		code := client.Procedure(
			ts.Server.CreateInvite,
			opensocial_api.CommunityOpensocialCreateInviteInput{
				Org: orgDID, Invitee: "not-a-did",
			},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("delete role requires a role", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		var out struct{}
		code := client.Procedure(
			ts.Server.DeleteRole,
			opensocial_api.CommunityOpensocialDeleteRoleInput{Org: orgDID},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("put role requires role and name", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		var out struct{}

		code := client.Procedure(
			ts.Server.PutRole,
			opensocial_api.CommunityOpensocialPutRoleInput{Org: orgDID},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)

		code = client.Procedure(
			ts.Server.PutRole,
			opensocial_api.CommunityOpensocialPutRoleInput{Org: orgDID, Role: "moderator"},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("update profile requires a name", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		var out struct{}
		code := client.Procedure(
			ts.Server.UpdateProfile,
			opensocial_api.CommunityOpensocialUpdateProfileInput{Org: orgDID},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("ejecting a non-member is rejected", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		var out struct{}
		code := client.Procedure(
			ts.Server.EjectMember,
			opensocial_api.CommunityOpensocialEjectMemberInput{
				Org: orgDID, Member: bob.String(),
			},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)

		req := httptest.NewRequest(
			http.MethodPost,
			"/xrpc/community.opensocial.ejectMember",
			strings.NewReader("{not json"),
		)
		w := httptest.NewRecorder()
		ts.Server.EjectMember(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestServer_LlmAuthenticationErrors(t *testing.T) {
	ts := newOpenSocialServer(t, admin)
	orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)

	do := func(server *pearserver_testutil.TestServer, method, path, body string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		w := httptest.NewRecorder()
		server.Server.ServeHTTP(w, req)
		return w.Code
	}

	t.Run("rejects an invalid org in the path", func(t *testing.T) {
		code := do(ts, http.MethodGet, "/llm/not-a-did/v1/models", "")
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("rejects a non-member", func(t *testing.T) {
		aliceTS := newOpenSocialServerAs(t, ts, alice, authn.ValidatorMethodServiceAuth)
		code := do(aliceTS, http.MethodGet, "/llm/"+orgDID+"/v1/models", "")
		require.Equal(t, http.StatusForbidden, code)
	})

	t.Run("rejects a malformed completion body", func(t *testing.T) {
		code := do(
			ts, http.MethodPost, "/llm/"+orgDID+"/v1/chat/completions", "{not json",
		)
		require.Equal(t, http.StatusBadRequest, code)
	})
}

func TestServer_LlmUpdateConnectionNotFound(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)
	ts := newOpenSocialServer(t, admin)
	orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)

	var out struct{}
	code := client.Procedure(
		ts.Server.UpdateLlmConnection,
		habitat.NetworkHabitatLlmUpdateConnectionInput{Org: orgDID, Id: "missing"},
		&out,
	)
	require.Equal(t, http.StatusNotFound, code)
}
