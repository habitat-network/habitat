package opensocial

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"
	group_api "github.com/habitat-network/habitat/api/atgrp"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	canonicalAboutCollection  syntax.NSID = "org.atgrp.about"
	canonicalConfigCollection syntax.NSID = "org.atgrp.config"
)

var (
	canonicalDisplayName = lexicon.SchemaString{MaxLength: new(640), MaxGraphemes: new(64)}
	canonicalDescription = lexicon.SchemaString{MaxLength: new(20000), MaxGraphemes: new(2000)}
	canonicalAvatar      = lexicon.SchemaBlob{
		Accept: []string{"image/png", "image/jpeg", "image/webp"}, MaxSize: new(2000000),
	}
)

// legacyCanonicalAbout projects known profile fields without editing the legacy
// record, dropping invalid fields, or uploading an existing avatar again.
func (s *Store) legacyCanonicalAbout(
	ctx context.Context,
	group syntax.DID,
) (group_api.OrgAtgrpAbout, error) {
	profileSpace := habitat_syntax.ConstructSpaceURI(group, AboutSpaceType, "self")
	record, err := s.spacesStore.GetRecord(ctx, profileSpace, group, ProfileCollection, "self")
	if err != nil {
		return group_api.OrgAtgrpAbout{}, fmt.Errorf("read legacy group profile: %w", err)
	}
	var profile opensocial_api.CommunityOpensocialProfile
	if err := decodeRecordValue(record.Value, &profile); err != nil {
		return group_api.OrgAtgrpAbout{}, fmt.Errorf("decode legacy group profile: %w", err)
	}
	about := group_api.OrgAtgrpAbout{
		DisplayName: profile.Name,
		Description: profile.Description,
		Avatar:      profile.Avatar,
	}
	if err := validateCanonicalAbout(about); err != nil {
		return group_api.OrgAtgrpAbout{}, err
	}
	return about, nil
}

func validateCanonicalAbout(about group_api.OrgAtgrpAbout) error {
	if strings.TrimSpace(about.DisplayName) == "" || !utf8.ValidString(about.DisplayName) {
		return errors.New("displayName must be nonempty UTF-8")
	}
	if err := canonicalDisplayName.Validate(about.DisplayName, 0); err != nil {
		return fmt.Errorf("invalid displayName: %w", err)
	}
	if !utf8.ValidString(about.Description) {
		return errors.New("description must be UTF-8")
	}
	if err := canonicalDescription.Validate(about.Description, 0); err != nil {
		return fmt.Errorf("invalid description: %w", err)
	}
	if about.Avatar != nil {
		if err := canonicalAvatar.Validate(*about.Avatar, 0); err != nil {
			return fmt.Errorf("invalid avatar: %w", err)
		}
	}
	if about.Website != "" {
		u, err := url.Parse(about.Website)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return errors.New("website must be an HTTP or HTTPS URL")
		}
	}
	return nil
}

// writeCanonicalMetadata initializes the required meta records. Use it only
// while creating the reserved spaces in the same transaction.
func writeCanonicalMetadata(
	ctx context.Context,
	store spaces.Store,
	meta habitat_syntax.SpaceURI,
	about group_api.OrgAtgrpAbout,
) error {
	if err := validateCanonicalAbout(about); err != nil {
		return err
	}
	for _, record := range []struct {
		collection syntax.NSID
		value      any
	}{
		{canonicalAboutCollection, about},
		{canonicalConfigCollection, group_api.OrgAtgrpConfig{JoinPolicy: "invite"}},
	} {
		value, err := spaces.MarshalRecord(record.value)
		if err != nil {
			return fmt.Errorf("marshal %s: %w", record.collection, err)
		}
		if _, _, err := store.PutRecord(
			ctx, meta, meta.SpaceOwner(), record.collection, "self", value,
		); err != nil {
			return fmt.Errorf("initialize %s: %w", record.collection, err)
		}
	}
	return nil
}
