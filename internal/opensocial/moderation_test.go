package opensocial_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestParseSubject(t *testing.T) {
	account, err := opensocial.ParseSubject("did:plc:alice", "")
	require.NoError(t, err)
	require.Equal(t, syntax.DID("did:plc:alice"), account.Author())
	require.Equal(t, "did:plc:alice", account.Key())

	record, err := opensocial.ParseSubject(
		"at:space/did:plc:org/network.habitat.docs/s/did:plc:alice/network.habitat.note/n1", "bafy",
	)
	require.NoError(t, err)
	require.Equal(t, syntax.DID("did:plc:alice"), record.Author())
	require.Equal(t, "bafy", record.CID)

	for _, bad := range []string{"", "did:not valid", "not-a-subject"} {
		_, err := opensocial.ParseSubject(bad, "")
		require.ErrorIs(t, err, opensocial.ErrInvalidSubject, bad)
	}
}

func TestStoreModeration(t *testing.T) {
	ctx := t.Context()
	s := opensocial_testutil.NewTestStore(t)
	moderator := syntax.DID("did:plc:mod")
	alice := syntax.DID("did:plc:alice")
	org, err := s.NewOrg(ctx, "acme", moderator)
	require.NoError(t, err)
	group := syntax.DID(org)
	require.NoError(t, s.AssignRoles(ctx, group, alice, []string{opensocial.MemberRoleRkey}))
	space, err := s.CreateSpace(
		ctx, group, []string{opensocial.MemberRoleRkey}, "network.habitat.docs", "shared",
	)
	require.NoError(t, err)
	record := opensocial.Subject{URI: habitat_syntax.ConstructSpaceRecordURI(
		space, alice, "network.habitat.note", "n1",
	)}
	spam := opensocial.Report{ReasonType: "org.atgrp.mod.defs#reasonSpam"}

	t.Run("reports open a subject, once per account or record", func(t *testing.T) {
		_, err := s.SubmitReport(ctx, group, alice, record, spam)
		require.NoError(t, err)
		_, err = s.SubmitReport(ctx, group, moderator, record, spam)
		require.NoError(t, err)
		_, err = s.SubmitReport(ctx, group, moderator, opensocial.Subject{DID: alice}, spam)
		require.NoError(t, err)

		subjects, err := s.ListSubjects(ctx, group, opensocial.SubjectFilter{}, 10, nil)
		require.NoError(t, err)
		require.Len(t, subjects, 2)
		var reported opensocial.SubjectView
		for _, subject := range subjects {
			if subject.Subject.URI != "" {
				reported = subject
			}
		}
		require.EqualValues(t, 2, reported.ReportCount)
		require.Equal(t, opensocial.StatusOpen, reported.Status)
	})

	t.Run("reports are refused for strangers and unreadable spaces", func(t *testing.T) {
		_, err := s.SubmitReport(
			ctx, group, alice, opensocial.Subject{DID: "did:plc:stranger"}, spam,
		)
		require.ErrorIs(t, err, opensocial.ErrInvalidSubject)
		_, err = s.SubmitReport(ctx, group, alice, opensocial.Subject{
			URI: habitat_syntax.ConstructSpaceRecordURI(
				habitat_syntax.ConstructSpaceURI("did:plc:other", "network.habitat.docs", "x"),
				alice, "network.habitat.note", "n1",
			),
		}, spam)
		require.ErrorIs(t, err, opensocial.ErrInvalidSubject)
		_, err = s.SubmitReport(ctx, group, "did:plc:outsider", record, spam)
		require.ErrorIs(t, err, opensocial.ErrNoReadAccess)
	})

	t.Run("a moderator resolves and tags, and the queue filters and sorts", func(t *testing.T) {
		view, err := s.UpdateSubject(ctx, group, moderator, record, opensocial.SubjectUpdate{
			Status: opensocial.StatusResolved, Note: "spam", AddTags: []string{"spam", "bot"},
		})
		require.NoError(t, err)
		require.Equal(t, opensocial.StatusResolved, view.Status)
		view, err = s.UpdateSubject(ctx, group, moderator, record, opensocial.SubjectUpdate{
			RemoveTags: []string{"bot"},
		})
		require.NoError(t, err)
		require.Equal(t, []string{"spam"}, view.Tags)

		resolved, err := s.ListSubjects(ctx, group, opensocial.SubjectFilter{
			Status: opensocial.StatusResolved, Tags: []string{"spam", "other"},
		}, 10, nil)
		require.NoError(t, err)
		require.Len(t, resolved, 1)
		none, err := s.ListSubjects(ctx, group, opensocial.SubjectFilter{
			Tags: []string{"other"},
		}, 10, nil)
		require.NoError(t, err)
		require.Empty(t, none)

		_, err = s.UpdateSubject(ctx, group, moderator, opensocial.Subject{DID: moderator},
			opensocial.SubjectUpdate{Status: opensocial.StatusOpen})
		require.ErrorIs(t, err, opensocial.ErrSubjectNotFound)
	})

	t.Run("listing pages through every sort order", func(t *testing.T) {
		for _, sort := range []opensocial.SortOrder{
			opensocial.SortOldest, opensocial.SortNewest, opensocial.SortUpdated,
		} {
			var seen []string
			var after *opensocial.SubjectCursor
			for range 3 {
				page, err := s.ListSubjects(
					ctx, group, opensocial.SubjectFilter{Sort: sort}, 1, after,
				)
				require.NoError(t, err)
				if len(page) == 0 {
					break
				}
				last := page[len(page)-1]
				at := last.CreatedAt
				if sort == opensocial.SortUpdated {
					at = last.UpdatedAt
				}
				after = &opensocial.SubjectCursor{
					At: at, Key: last.Subject.Key(), CID: last.Subject.CID,
				}
				seen = append(seen, last.Subject.Key())
			}
			require.Len(t, seen, 2, sort)
		}
	})

	t.Run("history lists events and can be filtered", func(t *testing.T) {
		history, err := s.GetHistory(ctx, group, opensocial.HistoryFilter{
			Subject: &record, Sort: opensocial.SortOldest,
		}, 10, "")
		require.NoError(t, err)
		require.Len(t, history, 5, "two reports, status, two tags")
		require.Equal(t, opensocial.EventReport, history[0].Kind)

		next, err := s.GetHistory(ctx, group, opensocial.HistoryFilter{
			Subject: &record, Sort: opensocial.SortOldest,
		}, 10, history[0].ID)
		require.NoError(t, err)
		require.Len(t, next, 4)

		reports, err := s.GetHistory(ctx, group, opensocial.HistoryFilter{
			Author: alice, Kinds: []opensocial.EventKind{opensocial.EventReport},
		}, 10, "")
		require.NoError(t, err)
		require.Len(t, reports, 3)

		_, err = s.GetHistory(ctx, group, opensocial.HistoryFilter{
			Subject: &opensocial.Subject{DID: "did:plc:stranger"},
		}, 10, "")
		require.ErrorIs(t, err, opensocial.ErrInvalidSubject)
	})

	t.Run("appeals need a label or an ejection and reopen the subject", func(t *testing.T) {
		_, err := s.SubmitAppeal(ctx, group, alice, record, "unfair")
		require.ErrorIs(t, err, opensocial.ErrNotAppealable)

		require.NoError(t, s.RecordLabel(
			ctx, group, group, record, "at://label/1", "spam", false,
		))
		_, err = s.SubmitAppeal(ctx, group, alice, record, "unfair")
		require.NoError(t, err)
		open, err := s.ListSubjects(ctx, group, opensocial.SubjectFilter{
			Status: opensocial.StatusOpen,
		}, 10, nil)
		require.NoError(t, err)
		require.Len(t, open, 2)

		_, err = s.SubmitAppeal(ctx, group, moderator, record, "not mine")
		require.ErrorIs(t, err, opensocial.ErrInvalidSubject)

		require.NoError(t, s.RecordEject(ctx, group, moderator, alice))
		_, err = s.SubmitAppeal(ctx, group, alice, opensocial.Subject{DID: alice}, "back")
		require.NoError(t, err)
	})
}
