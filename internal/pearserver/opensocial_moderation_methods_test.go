package pearserver_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
)

func TestServer_GroupModerationMethods(t *testing.T) {
	adminTS, aliceTS, _ := newSharedOpenSocialServers(t)
	org, err := adminTS.OpenSocialStore.NewOrg(t.Context(), "mod-acme", admin)
	require.NoError(t, err)
	require.NoError(t, adminTS.OpenSocialStore.AssignRoles(
		t.Context(), syntax.DID(org), alice, []string{opensocial.MemberRoleRkey},
	))
	client := httpx_testutil.NewTestXRPCClient(t)
	subject := map[string]any{"$type": "group.opensocial.defs#accountSubject", "did": string(admin)}

	var report opensocial_api.GroupOpensocialCreateReportOutput
	require.Equal(
		t,
		http.StatusOK,
		client.Procedure(aliceTS.Server.GroupCreateReport, map[string]any{
			"group":      org,
			"subject":    subject,
			"reasonType": "com.atproto.moderation.defs#reasonSpam",
		}, &report),
	)
	require.NotEmpty(t, report.Id)

	t.Run("a member cannot read the queue but an admin can", func(t *testing.T) {
		var out opensocial_api.GroupOpensocialListSubjectsOutput
		require.Equal(t, http.StatusUnauthorized,
			client.Query(aliceTS.Server.GroupListSubjects, url.Values{"group": {org}}, &out))
		require.Equal(t, http.StatusOK,
			client.Query(adminTS.Server.GroupListSubjects, url.Values{"group": {org}}, &out))
		require.Len(t, out.Subjects, 1)
		require.EqualValues(t, 1, out.Subjects[0].ReportCount)
	})

	t.Run("an admin resolves the subject and reads its history", func(t *testing.T) {
		var resolved opensocial_api.GroupOpensocialDefsSubjectView
		require.Equal(
			t,
			http.StatusOK,
			client.Procedure(adminTS.Server.GroupResolveSubject, map[string]any{
				"group": org, "subject": subject, "status": "resolved", "note": "done",
			}, &resolved),
		)
		require.Equal(t, "resolved", resolved.Status)
		var out opensocial_api.GroupOpensocialGetSubjectHistoryOutput
		require.Equal(t, http.StatusOK, client.Query(adminTS.Server.GroupGetSubjectHistory,
			url.Values{"group": {org}, "subject": {string(admin)}}, &out))
		require.Len(t, out.Events, 2)
	})

	t.Run("a stranger is an invalid subject", func(t *testing.T) {
		var ignored opensocial_api.GroupOpensocialCreateReportOutput
		code := client.Procedure(aliceTS.Server.GroupCreateReport, map[string]any{
			"group": org, "subject": map[string]any{"did": "did:plc:stranger"},
			"reasonType": "com.atproto.moderation.defs#reasonSpam",
		}, &ignored)
		require.Equal(t, http.StatusBadRequest, code)
	})
}
