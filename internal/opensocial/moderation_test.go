package opensocial_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/opensocial"
)

func TestModeration_ReportResolveHistory(t *testing.T) {
	s := newTestStore(t)
	org, err := s.NewOrg(t.Context(), "mod-acme", alice)
	require.NoError(t, err)
	group := syntax.DID(org)
	subject := opensocial.Subject{DID: alice}

	_, err = s.CreateReport(t.Context(), group, bob, subject, opensocial.Report{ReasonType: "spam"})
	require.NoError(t, err)
	_, err = s.CreateReport(t.Context(), group, bob, subject, opensocial.Report{ReasonType: "other"})
	require.NoError(t, err)

	open, err := s.ListSubjects(t.Context(), group, opensocial.StatusOpen, 10, nil)
	require.NoError(t, err)
	require.Len(t, open, 1)
	require.EqualValues(t, 2, open[0].ReportCount)

	view, err := s.ResolveSubject(
		t.Context(), group, alice, subject, opensocial.StatusResolved, "handled",
	)
	require.NoError(t, err)
	require.Equal(t, opensocial.StatusResolved, view.Status)

	open, err = s.ListSubjects(t.Context(), group, opensocial.StatusOpen, 10, nil)
	require.NoError(t, err)
	require.Empty(t, open)

	// A new report reopens the subject.
	_, err = s.CreateReport(t.Context(), group, bob, subject, opensocial.Report{ReasonType: "spam"})
	require.NoError(t, err)
	open, err = s.ListSubjects(t.Context(), group, opensocial.StatusOpen, 10, nil)
	require.NoError(t, err)
	require.Len(t, open, 1)

	history, err := s.GetSubjectHistory(t.Context(), group, subject, 10, "")
	require.NoError(t, err)
	require.Len(t, history, 4)
	require.Equal(t, opensocial.EventStatus, history[2].Kind)
	after, err := s.GetSubjectHistory(t.Context(), group, subject, 10, history[2].ID)
	require.NoError(t, err)
	require.Len(t, after, 1)
}

func TestModeration_InvalidAndUnknownSubjects(t *testing.T) {
	s := newTestStore(t)
	org, err := s.NewOrg(t.Context(), "mod-other", alice)
	require.NoError(t, err)
	group := syntax.DID(org)

	_, err = s.CreateReport(
		t.Context(), group, alice, opensocial.Subject{DID: bob}, opensocial.Report{ReasonType: "spam"},
	)
	require.ErrorIs(t, err, opensocial.ErrInvalidSubject)

	_, err = s.CreateReport(
		t.Context(), group, alice,
		opensocial.Subject{URI: "at://did:plc:elsewhere/space/group.opensocial.meta/self/did:plc:x/a.b/1"},
		opensocial.Report{ReasonType: "spam"},
	)
	require.ErrorIs(t, err, opensocial.ErrInvalidSubject)

	_, err = s.ResolveSubject(
		t.Context(), group, alice, opensocial.Subject{DID: alice},
		opensocial.StatusResolved, "",
	)
	require.ErrorIs(t, err, opensocial.ErrSubjectNotFound)
}
