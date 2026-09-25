package spaces_test

import (
	"testing"

	"github.com/habitat-network/habitat/internal/spaces"
	spacestest "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/stretchr/testify/require"
)

func TestMailboxSnapshotRequiresExactSpaceType(t *testing.T) {
	store := spacestest.NewTestStore(t)
	_, err := store.CreateSpace(t.Context(), orgID, groupType, "same-key")
	require.NoError(t, err)
	missing := habitat_syntax.SpaceURI(
		"at://" + orgID.String() + "/space/email.atmos.mailbox/same-key",
	)
	_, _, err = store.RepoSnapshot(t.Context(), missing, owner)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}

func TestMailboxDeleteSpacePreservesOtherSpaceType(t *testing.T) {
	store := spacestest.NewTestStore(t)
	mailbox, err := store.CreateSpace(t.Context(), orgID, "email.atmos.mailbox", "same-key")
	require.NoError(t, err)
	other, err := store.CreateSpace(t.Context(), orgID, groupType, "same-key")
	require.NoError(t, err)
	value := spacestest.MustMarshalRecord(t, map[string]any{"text": "synthetic"})
	_, _, err = store.PutRecord(t.Context(), mailbox, owner, "email.atmos.message", "mail", value)
	require.NoError(t, err)
	_, _, err = store.PutRecord(t.Context(), other, owner, "com.example.note", "note", value)
	require.NoError(t, err)
	require.NoError(t, store.DeleteSpace(t.Context(), other))
	exists, err := store.CheckSpaceExists(t.Context(), mailbox)
	require.NoError(t, err)
	require.True(t, exists, "deleting a different Space type must preserve the mailbox")
	_, err = store.GetRecord(t.Context(), mailbox, owner, "email.atmos.message", "mail")
	require.NoError(t, err)
}
