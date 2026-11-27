package pearserver_test

import (
	"fmt"
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

func TestServer_GroupModerationEdgeCases(t *testing.T) {
	adminTS, aliceTS, _ := newSharedOpenSocialServers(t)
	org, err := adminTS.OpenSocialStore.NewOrg(t.Context(), "mod-edge", admin)
	require.NoError(t, err)
	require.NoError(t, adminTS.OpenSocialStore.AssignRoles(
		t.Context(), syntax.DID(org), alice, []string{opensocial.MemberRoleRkey},
	))
	client := httpx_testutil.NewTestXRPCClient(t)
	const spam = "com.atproto.moderation.defs#reasonSpam"
	record := fmt.Sprintf(
		"at://%s/space/group.opensocial.meta/self/%s/group.opensocial.profile/self", org, org,
	)
	report := func(body map[string]any) int {
		var out opensocial_api.GroupOpensocialCreateReportOutput
		return client.Procedure(aliceTS.Server.GroupCreateReport, body, &out)
	}

	t.Run("reports are validated", func(t *testing.T) {
		account := map[string]any{"did": string(admin)}
		require.Equal(t, http.StatusBadRequest,
			report(map[string]any{"group": org, "subject": map[string]any{}, "reasonType": spam}))
		require.Equal(t, http.StatusBadRequest,
			report(map[string]any{"group": org, "subject": account}))
		require.Equal(
			t,
			http.StatusNotFound,
			report(
				map[string]any{"group": "did:plc:nowhere", "subject": account, "reasonType": spam},
			),
		)
		require.Equal(t, http.StatusBadRequest, report(map[string]any{
			"group":      org,
			"subject":    map[string]any{"uri": "at://did:plc:x/space/a.b/self/did:plc:x/a.b/1"},
			"reasonType": spam,
		}))
	})

	t.Run("a report on an unknown group is refused", func(t *testing.T) {
		var out opensocial_api.GroupOpensocialCreateReportOutput
		code := client.Procedure(adminTS.Server.GroupCreateReport, map[string]any{
			"group": "did:plc:nowhere", "subject": map[string]any{"did": string(admin)},
			"reasonType": spam,
		}, &out)
		require.Equal(t, http.StatusNotFound, code)
	})

	t.Run("a record subject is reported, listed and resolved", func(t *testing.T) {
		require.Equal(t, http.StatusOK, report(map[string]any{
			"group": org, "subject": map[string]any{"uri": record, "cid": "bafycid"},
			"reasonType": spam, "reason": "bad",
		}))
		var queue opensocial_api.GroupOpensocialListSubjectsOutput
		require.Equal(t, http.StatusOK, client.Query(
			adminTS.Server.GroupListSubjects, url.Values{"group": {org}, "limit": {"1"}}, &queue,
		))
		require.Len(t, queue.Subjects, 1)
		require.NotEmpty(t, queue.Cursor)
		var next opensocial_api.GroupOpensocialListSubjectsOutput
		require.Equal(t, http.StatusOK, client.Query(adminTS.Server.GroupListSubjects,
			url.Values{"group": {org}, "limit": {"1"}, "cursor": {queue.Cursor}}, &next))
		require.Empty(t, next.Subjects)

		var history opensocial_api.GroupOpensocialGetSubjectHistoryOutput
		require.Equal(t, http.StatusOK, client.Query(adminTS.Server.GroupGetSubjectHistory,
			url.Values{"group": {org}, "subject": {record}, "cid": {"bafycid"}, "limit": {"1"}},
			&history))
		require.Len(t, history.Events, 1)
		require.NotEmpty(t, history.Cursor)

		var resolved opensocial_api.GroupOpensocialDefsSubjectView
		require.Equal(t, http.StatusOK, client.Procedure(adminTS.Server.GroupResolveSubject,
			map[string]any{
				"group": org, "subject": map[string]any{"uri": record, "cid": "bafycid"},
				"status": "escalated",
			}, &resolved))
		require.Equal(t, "escalated", resolved.Status)
		var escalated opensocial_api.GroupOpensocialListSubjectsOutput
		require.Equal(t, http.StatusOK, client.Query(adminTS.Server.GroupListSubjects,
			url.Values{"group": {org}, "status": {"escalated"}}, &escalated))
		require.Len(t, escalated.Subjects, 1)
	})

	t.Run("queries and resolutions are validated", func(t *testing.T) {
		var queue opensocial_api.GroupOpensocialListSubjectsOutput
		for _, params := range []url.Values{
			{"group": {org}, "status": {"bogus"}},
			{"group": {org}, "cursor": {"!!"}},
			{"group": {org}, "limit": {"0"}},
			{"group": {"not-a-did"}},
		} {
			require.Equal(t, http.StatusBadRequest,
				client.Query(adminTS.Server.GroupListSubjects, params, &queue))
		}
		var history opensocial_api.GroupOpensocialGetSubjectHistoryOutput
		require.Equal(t, http.StatusBadRequest, client.Query(
			adminTS.Server.GroupGetSubjectHistory, url.Values{"group": {org}}, &history,
		))
		require.Equal(t, http.StatusBadRequest, client.Query(adminTS.Server.GroupGetSubjectHistory,
			url.Values{"group": {org}, "subject": {"did:plc:stranger"}}, &history))
		require.Equal(t, http.StatusBadRequest, client.Query(adminTS.Server.GroupGetSubjectHistory,
			url.Values{"group": {org}, "subject": {string(admin)}, "limit": {"500"}}, &history))

		var view opensocial_api.GroupOpensocialDefsSubjectView
		for _, body := range []map[string]any{
			{"group": org, "subject": map[string]any{}, "status": "resolved"},
			{"group": org, "subject": map[string]any{"did": string(admin)}, "status": "open"},
		} {
			require.Equal(t, http.StatusBadRequest,
				client.Procedure(adminTS.Server.GroupResolveSubject, body, &view))
		}
		require.Equal(t, http.StatusBadRequest, client.Procedure(adminTS.Server.GroupResolveSubject,
			map[string]any{
				"group":   org,
				"subject": map[string]any{"did": "did:plc:stranger"},
				"status":  "resolved",
			}, &view))
		require.Equal(t, http.StatusNotFound, client.Procedure(adminTS.Server.GroupResolveSubject,
			map[string]any{
				"group": org, "subject": map[string]any{"did": string(admin)}, "status": "resolved",
			}, &view))
	})
}
