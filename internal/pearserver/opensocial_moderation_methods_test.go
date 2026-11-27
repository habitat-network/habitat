package pearserver_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestServer_Moderation(t *testing.T) {
	adminTS, aliceTS, _ := newSharedOpenSocialServers(t)
	org, err := adminTS.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)
	group := syntax.DID(org)
	for _, member := range []syntax.DID{alice} {
		require.NoError(t, adminTS.OpenSocialStore.AssignRoles(
			t.Context(), group, member, []string{opensocial.MemberRoleRkey},
		))
	}
	groupTS := newOpenSocialServerAs(t, adminTS, group, authn.ValidatorMethodOAuth)
	client := httpx_testutil.NewTestXRPCClient(t)

	// A record alice wrote into a space every member reads.
	space, err := adminTS.OpenSocialStore.CreateSpace(
		t.Context(), group, []string{opensocial.MemberRoleRkey}, "network.habitat.docs", "shared",
	)
	require.NoError(t, err)
	record := habitat_syntax.ConstructSpaceRecordURI(
		space,
		alice,
		"network.habitat.docs.note",
		"n1",
	)

	report := func(subject string) opensocial_api.OrgAtgrpModSubmitReportInput {
		return opensocial_api.OrgAtgrpModSubmitReportInput{
			Group: org, Subject: subject, ReasonType: "org.atgrp.mod.defs#reasonSpam",
		}
	}
	var reported opensocial_api.OrgAtgrpModSubmitReportOutput
	queue := func(params url.Values) opensocial_api.OrgAtgrpModQuerySubjectsOutput {
		var out opensocial_api.OrgAtgrpModQuerySubjectsOutput
		require.Equal(t, http.StatusOK, client.Query(groupTS.Server.ModQuerySubjects, params, &out))
		return out
	}

	t.Run("a report opens a subject in the queue", func(t *testing.T) {
		require.Equal(t, http.StatusOK,
			client.Procedure(aliceTS.Server.ModSubmitReport, report(record.String()), &reported))
		require.NotEmpty(t, reported.EventId)
		subjects := queue(nil).Subjects
		require.Len(t, subjects, 1)
		require.Equal(t, record.AtSpace().String(), subjects[0].Subject)
		require.Equal(t, "open", subjects[0].Status)
		require.EqualValues(t, 1, subjects[0].ReportCount)
	})

	t.Run("reports are refused for bad subjects", func(t *testing.T) {
		var out opensocial_api.OrgAtgrpModSubmitReportOutput
		require.Equal(t, http.StatusBadRequest,
			client.Procedure(aliceTS.Server.ModSubmitReport, report("not-a-subject"), &out))
		elsewhere := habitat_syntax.ConstructSpaceRecordURI(
			habitat_syntax.ConstructSpaceURI("did:plc:other", "network.habitat.docs", "x"),
			alice, "network.habitat.docs.note", "n1",
		)
		require.Equal(t, http.StatusBadRequest,
			client.Procedure(aliceTS.Server.ModSubmitReport, report(elsewhere.String()), &out))
		require.Equal(t, http.StatusBadRequest,
			client.Procedure(aliceTS.Server.ModSubmitReport, report("did:plc:stranger"), &out))
	})

	t.Run("only the group updates the queue", func(t *testing.T) {
		require.Equal(t, http.StatusUnauthorized, client.Procedure(
			aliceTS.Server.ModUpdateSubject,
			opensocial_api.OrgAtgrpModUpdateSubjectInput{
				Subject: record.String(),
				Status:  "resolved",
			},
			&struct{}{},
		))
	})

	t.Run("a moderator resolves, tags and filters the queue", func(t *testing.T) {
		require.Equal(t, http.StatusOK, client.Procedure(
			groupTS.Server.ModUpdateSubject,
			opensocial_api.OrgAtgrpModUpdateSubjectInput{
				Subject: record.String(), Status: "resolved", Note: "spam",
				AddTags: []string{"spam", "bot"},
			}, &struct{}{}))
		require.Equal(t, http.StatusOK, client.Procedure(
			groupTS.Server.ModUpdateSubject,
			opensocial_api.OrgAtgrpModUpdateSubjectInput{
				Subject: record.String(), RemoveTags: []string{"bot"},
			}, &struct{}{}))

		require.Empty(t, queue(url.Values{"status": {"open"}}).Subjects)
		resolved := queue(url.Values{"status": {"resolved"}, "tags": {"spam", "other"}}).Subjects
		require.Len(t, resolved, 1)
		require.Equal(t, []string{"spam"}, resolved[0].Tags)
		require.Empty(t, queue(url.Values{"tags": {"other"}}).Subjects)

		require.Equal(t, http.StatusNotFound, client.Procedure(
			groupTS.Server.ModUpdateSubject,
			opensocial_api.OrgAtgrpModUpdateSubjectInput{Subject: alice.String(), Status: "open"},
			&struct{}{}))
	})

	t.Run(
		"history shows moderators every event and the member a redacted view",
		func(t *testing.T) {
			var history opensocial_api.OrgAtgrpModQuerySubjectHistoryOutput
			require.Equal(t, http.StatusOK, client.Query(
				groupTS.Server.ModQuerySubjectHistory,
				url.Values{"subject": {record.String()}, "sort": {"oldest"}}, &history))
			require.Len(t, history.Events, 4, "report, status, two tag changes")
			require.Equal(t, alice.String(), history.Events[0].CreatedBy)

			var own opensocial_api.OrgAtgrpModQuerySelfHistoryOutput
			require.Equal(t, http.StatusOK, client.Query(
				aliceTS.Server.ModQuerySelfHistory, url.Values{"group": {org}}, &own))
			require.Len(t, own.Events, 1, "only the status change, without its note or author")
			require.Empty(t, own.Events[0].CreatedBy)
			event, err := json.Marshal(own.Events[0].Event)
			require.NoError(t, err)
			require.JSONEq(
				t, `{"$type":"org.atgrp.mod.defs#statusEvent","status":"resolved"}`, string(event),
			)
		},
	)

	t.Run("an appeal needs a label or an ejection and reopens the subject", func(t *testing.T) {
		appeal := opensocial_api.OrgAtgrpModSubmitAppealInput{
			Group: org, Subject: record.String(), Note: "unfair",
		}
		var out opensocial_api.OrgAtgrpModSubmitAppealOutput
		require.Equal(t, http.StatusBadRequest,
			client.Procedure(aliceTS.Server.ModSubmitAppeal, appeal, &out), "nothing to appeal yet")

		require.NoError(t, adminTS.OpenSocialStore.RecordLabel(
			t.Context(),
			group,
			group,
			opensocial.Subject{URI: record},
			"at://label/1",
			"spam",
			false,
		))
		require.Equal(
			t,
			http.StatusOK,
			client.Procedure(aliceTS.Server.ModSubmitAppeal, appeal, &out),
		)
		require.Len(t, queue(url.Values{"status": {"open"}}).Subjects, 1)

		appeal.Subject = group.String()
		require.Equal(t, http.StatusBadRequest,
			client.Procedure(aliceTS.Server.ModSubmitAppeal, appeal, &out), "not the caller's")
	})

	t.Run("an ejected member can still read their history and appeal", func(t *testing.T) {
		require.Equal(t, http.StatusOK, client.Procedure(
			adminTS.Server.EjectMember,
			habitatEject(org, alice), &struct{}{}))
		var own opensocial_api.OrgAtgrpModQuerySelfHistoryOutput
		require.Equal(t, http.StatusOK, client.Query(
			aliceTS.Server.ModQuerySelfHistory,
			url.Values{"group": {org}, "subject": {alice.String()}}, &own))
		require.Len(t, own.Events, 1)

		var out opensocial_api.OrgAtgrpModSubmitAppealOutput
		require.Equal(t, http.StatusOK, client.Procedure(
			aliceTS.Server.ModSubmitAppeal,
			opensocial_api.OrgAtgrpModSubmitAppealInput{Group: org, Subject: alice.String()}, &out))
	})
}

func TestServer_ModerationPaging(t *testing.T) {
	adminTS, _, _ := newSharedOpenSocialServers(t)
	org, err := adminTS.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)
	group := syntax.DID(org)
	groupTS := newOpenSocialServerAs(t, adminTS, group, authn.ValidatorMethodOAuth)
	client := httpx_testutil.NewTestXRPCClient(t)

	for _, member := range []syntax.DID{"did:plc:a", "did:plc:b", "did:plc:c"} {
		require.NoError(t, adminTS.OpenSocialStore.AssignRoles(
			t.Context(), group, member, []string{opensocial.MemberRoleRkey}))
		_, err := adminTS.OpenSocialStore.SubmitReport(
			t.Context(), group, admin, opensocial.Subject{DID: member},
			opensocial.Report{ReasonType: "org.atgrp.mod.defs#reasonOther"})
		require.NoError(t, err)
	}

	for _, sort := range []string{"oldest", "newest", "updated"} {
		var seen []string
		cursor := ""
		for range 4 {
			var out opensocial_api.OrgAtgrpModQuerySubjectsOutput
			require.Equal(t, http.StatusOK, client.Query(
				groupTS.Server.ModQuerySubjects,
				url.Values{"limit": {"2"}, "cursor": {cursor}, "sort": {sort}}, &out))
			for _, subject := range out.Subjects {
				seen = append(seen, subject.Subject)
			}
			if len(out.Subjects) < 2 {
				break
			}
			cursor = out.Cursor
		}
		require.ElementsMatchf(t, []string{"did:plc:a", "did:plc:b", "did:plc:c"}, seen, sort)
		require.Len(t, seen, 3, sort)
	}

	var out opensocial_api.OrgAtgrpModQuerySubjectsOutput
	for _, bad := range []url.Values{
		{"cursor": {"junk"}}, {"limit": {"500"}}, {"sort": {"sideways"}}, {"status": {"escalated"}},
	} {
		require.Equal(t, http.StatusBadRequest,
			client.Query(groupTS.Server.ModQuerySubjects, bad, &out), bad.Encode())
	}
	var history opensocial_api.OrgAtgrpModQuerySubjectHistoryOutput
	require.Equal(t, http.StatusBadRequest, client.Query(
		groupTS.Server.ModQuerySubjectHistory,
		url.Values{"subject": {"did:plc:a"}, "cursor": {"junk"}}, &history))
}

func habitatEject(org string, member syntax.DID) any {
	return map[string]string{"org": org, "member": member.String()}
}
