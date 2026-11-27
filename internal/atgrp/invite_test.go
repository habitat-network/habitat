package atgrp_test

import (
	"errors"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
	"github.com/habitat-network/habitat/internal/atgrp"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	inviteGroup = syntax.DID("did:plc:group")
	invitee     = syntax.DID("did:plc:alice")
	inviteKey   = syntax.RecordKey("3jzfcijpj2z2a")
	inviteNSID  = atgrp.InviteCollection
)

func TestParseBoundInvite(t *testing.T) {
	construct := habitat_syntax.ConstructSpaceRecordURI
	inbox := atgrp.InboxSpace(invitee)
	expected := construct(inbox, inviteGroup, inviteNSID, inviteKey)
	tests := []struct {
		name string
		uri  string
		ok   bool
	}{
		{"spec wire URI", expected.AtSpace().String(), true},
		{
			"repo and recipient reversed",
			construct(
				atgrp.InboxSpace(inviteGroup),
				invitee,
				inviteNSID,
				inviteKey,
			).AtSpace().
				String(),
			false,
		},
		{
			"other recipient",
			construct(atgrp.InboxSpace("did:plc:bob"), inviteGroup, inviteNSID, inviteKey).String(),
			false,
		},
		{"other group", construct(inbox, "did:plc:other", inviteNSID, inviteKey).String(), false},
		{
			"wrong collection",
			construct(inbox, inviteGroup, "org.atgrp.membership", inviteKey).String(),
			false,
		},
		{"wrong key", construct(inbox, inviteGroup, inviteNSID, "not-a-tid").String(), false},
		{
			"wrong space type",
			construct(
				habitat_syntax.ConstructSpaceURI(invitee, "org.atgrp.members", "self"),
				inviteGroup,
				inviteNSID,
				inviteKey,
			).String(),
			false,
		},
		{
			"wrong space key",
			construct(
				habitat_syntax.ConstructSpaceURI(invitee, atgrp.InboxSpaceType, "other"),
				inviteGroup,
				inviteNSID,
				inviteKey,
			).String(),
			false,
		},
		{"regular AT URI", "at://did:plc:group/org.atgrp.invite/3jzfcijpj2z2a", false},
		{"garbage", "garbage", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := atgrp.ParseBoundInvite(tt.uri, inviteGroup, invitee)
			if tt.ok {
				require.NoError(t, err)
				require.Equal(t, expected, actual)
			} else {
				require.ErrorIs(t, err, atgrp.ErrInvalidInvite)
			}
		})
	}
}

func TestValidateInviteRecord(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	valid := map[string]any{"$type": inviteNSID, "createdAt": "2026-11-27T11:00:00Z"}
	require.NoError(t, atgrp.ValidateInviteRecord(valid, now))

	for _, malformed := range []any{7, "", "bogus", nil, "2026-11-27T11:00:00Z", "2026-11-27T12:00:00Z"} {
		valid["expiresAt"] = malformed
		require.ErrorIs(t, atgrp.ValidateInviteRecord(valid, now), atgrp.ErrInvalidInvite)
	}
	valid["expiresAt"] = "2026-11-27T12:00:00.000001Z"
	require.NoError(t, atgrp.ValidateInviteRecord(valid, now))
	valid["$type"] = "org.atgrp.membership"
	require.ErrorIs(t, atgrp.ValidateInviteRecord(valid, now), atgrp.ErrInvalidInvite)
	valid["$type"] = inviteNSID
	delete(valid, "createdAt")
	require.ErrorIs(t, atgrp.ValidateInviteRecord(valid, now), atgrp.ErrInvalidInvite)
}

func TestConsumeInvite(t *testing.T) {
	ctx := t.Context()
	db := pear_testutil.NewPearDB(t)
	store := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(db))
	space, err := store.CreateSpace(ctx, invitee, atgrp.InboxSpaceType, "self")
	require.NoError(t, err)

	now := time.Now().UTC()
	value, err := spaces.MarshalRecord(map[string]any{
		"$type":     inviteNSID,
		"createdAt": now.Format(time.RFC3339Nano),
	})
	require.NoError(t, err)
	uri, _, err := store.PutRecord(ctx, space, inviteGroup, inviteNSID, inviteKey, value)
	require.NoError(t, err)
	require.ErrorIs(t, atgrp.ConsumeInvite(ctx, store, uri, now), atgrp.ErrInvalidInvite,
		"an inbox needs its private public-write policy to hold redeemable invites")
	require.NoError(t, store.SetSpacePolicy(ctx, space, spaces.Policy{
		Type: spaces.PolicyMemberSpace, PublicWrite: true,
	}))

	abort := errors.New("membership write failed")
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		require.NoError(t, atgrp.ConsumeInvite(ctx, store.WithTx(tx), uri, now))
		return abort
	})
	require.ErrorIs(t, err, abort)
	_, err = store.GetRecord(ctx, space, inviteGroup, inviteNSID, inviteKey)
	require.NoError(t, err, "failed admission must leave the invite available")

	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return atgrp.ConsumeInvite(ctx, store.WithTx(tx), uri, now)
	})
	require.NoError(t, err)
	require.ErrorIs(t, atgrp.ConsumeInvite(ctx, store, uri, now), atgrp.ErrInvalidInvite)
}
