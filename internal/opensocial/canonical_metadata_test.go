package opensocial

import (
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	group_api "github.com/habitat-network/habitat/api/atgrp"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
)

func TestProjectCanonicalMetadata(t *testing.T) {
	ctx := t.Context()
	store := spaces_testutil.NewTestStore(t)
	group := syntax.DID("did:web:example.com")
	legacy, err := store.CreateSpace(ctx, group, AboutSpaceType, "self")
	require.NoError(t, err)
	meta, err := store.CreateSpace(ctx, group, "org.atgrp.meta", "self")
	require.NoError(t, err)

	cid, size, err := spaces_testutil.NewTestBlobStore(t).
		PutBlob(ctx, "image/png", []byte("avatar"))
	require.NoError(t, err)
	profile := opensocial_api.CommunityOpensocialProfile{
		Name:        "Workshop",
		Description: "A local group.",
		Avatar:      &atdata.Blob{Ref: atdata.CIDLink(cid), MimeType: "image/png", Size: size},
	}
	_, originalCID, err := store.PutRecord(
		ctx,
		legacy,
		group,
		ProfileCollection,
		"self",
		spaces_testutil.MustMarshalRecord(t, profile),
	)
	require.NoError(t, err)

	about, err := (&Store{spacesStore: store}).legacyCanonicalAbout(ctx, group)
	require.NoError(t, err)
	require.NoError(t, writeCanonicalMetadata(ctx, store, meta, about))

	record, err := store.GetRecord(ctx, meta, group, "org.atgrp.about", "self")
	require.NoError(t, err)
	var got group_api.OrgAtgrpAbout
	require.NoError(t, decodeRecordValue(record.Value, &got))
	require.Equal(t, "org.atgrp.about", got.LexiconTypeID)
	require.Equal(t, "Workshop", got.DisplayName)
	require.Equal(t, "A local group.", got.Description)
	require.Equal(t, profile.Avatar, got.Avatar)

	config, err := store.GetRecord(ctx, meta, group, "org.atgrp.config", "self")
	require.NoError(t, err)
	require.Equal(
		t,
		map[string]any{"$type": "org.atgrp.config", "joinPolicy": "invite"},
		config.Value,
	)

	legacyRecord, err := store.GetRecord(ctx, legacy, group, ProfileCollection, "self")
	require.NoError(t, err)
	require.Equal(t, *originalCID, legacyRecord.Cid)
}

func TestValidateCanonicalAbout(t *testing.T) {
	t.Run("enforces grapheme boundaries rather than rune count", func(t *testing.T) {
		cluster := "e\u0301"
		require.NoError(
			t,
			validateCanonicalAbout(
				group_api.OrgAtgrpAbout{DisplayName: strings.Repeat(cluster, 64)},
			),
		)
		require.Error(
			t,
			validateCanonicalAbout(
				group_api.OrgAtgrpAbout{DisplayName: strings.Repeat(cluster, 65)},
			),
		)
	})
	t.Run("enforces UTF-8 byte limit independently", func(t *testing.T) {
		cluster := "x" + strings.Repeat("\u0301", 320)
		require.Error(t, validateCanonicalAbout(group_api.OrgAtgrpAbout{DisplayName: cluster}))
	})
	t.Run("does not invent missing names or accept unsupported avatars", func(t *testing.T) {
		require.Error(t, validateCanonicalAbout(group_api.OrgAtgrpAbout{DisplayName: " "}))
		require.Error(t, validateCanonicalAbout(group_api.OrgAtgrpAbout{
			DisplayName: "Workshop", Avatar: &atdata.Blob{MimeType: "image/gif", Size: 50},
		}))
	})
}

func TestLegacyCanonicalAboutMissingProfile(t *testing.T) {
	ctx := t.Context()
	store := spaces_testutil.NewTestStore(t)
	group := syntax.DID("did:web:example.com")
	_, err := store.CreateSpace(ctx, group, AboutSpaceType, "self")
	require.NoError(t, err)
	_, err = (&Store{spacesStore: store}).legacyCanonicalAbout(ctx, group)
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
}
