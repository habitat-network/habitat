package opensocial

import (
	"context"
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"

	group_api "github.com/habitat-network/habitat/api/atgrp"
	"github.com/habitat-network/habitat/internal/atgrp"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type canonicalGroupRow struct {
	DID syntax.DID `gorm:"column:did;primaryKey;not null"`
}

// IsCanonicalGroup reports whether this host finished provisioning the DID as
// a canonical group. Staged or synced records do not select the auth model.
func (s *Store) IsCanonicalGroup(ctx context.Context, group syntax.DID) (bool, error) {
	var row canonicalGroupRow
	result := s.db.WithContext(ctx).Where("did = ?", group).Limit(1).Find(&row)
	if result.Error != nil {
		return false, fmt.Errorf("look up canonical group: %w", result.Error)
	}
	return result.RowsAffected != 0, nil
}

// NewCanonicalGroup provisions a group from a verified creator. Only the host
// may call it after authenticating the creator and checking its creation
// rules. It neither changes legacy groups nor issues OAuth credentials.
func (s *Store) NewCanonicalGroup(
	ctx context.Context,
	handle string,
	creator syntax.DID,
	about group_api.OrgAtgrpAbout,
) (syntax.DID, error) {
	if strings.Contains(handle, ".") || handle != strings.ToLower(handle) {
		return "", identity.ErrInvalidHandle
	}
	if _, err := syntax.ParseHandle(handle + ".example"); err != nil {
		return "", identity.ErrInvalidHandle
	}
	if _, err := syntax.ParseDID(creator.String()); err != nil {
		return "", fmt.Errorf("invalid creator DID: %w", err)
	}
	if err := validateCanonicalAbout(about); err != nil {
		return "", err
	}

	var group syntax.DID
	err := spaces.Transaction(ctx, s.db, func(tx *gorm.DB) error {
		id, err := s.hive.WithTx(tx).MintOrgIdentity(ctx, handle)
		if err != nil {
			return fmt.Errorf("mint group identity: %w", err)
		}
		group = id.DID
		groupSpaces := s.spacesStore.WithTx(tx)
		meta, err := createCanonicalConfigurationSpace(
			ctx, groupSpaces, group, habitat_syntax.AtgrpMetaSpaceType, false,
		)
		if err != nil {
			return err
		}
		if _, err := createCanonicalConfigurationSpace(
			ctx, groupSpaces, group, habitat_syntax.AtgrpMembersSpaceType, true,
		); err != nil {
			return err
		}
		if err := writeCanonicalMetadata(ctx, groupSpaces, meta, about); err != nil {
			return err
		}
		// Bootstrap takes the group lock after its members space exists. The
		// newly minted DID and both spaces are still private to this transaction.
		if err := atgrp.NewStore(tx, groupSpaces).Bootstrap(ctx, group, creator); err != nil {
			return err
		}
		if err := tx.Create(&canonicalGroupRow{DID: group}).Error; err != nil {
			return fmt.Errorf("record canonical provisioning: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("provision group: %w", err)
	}
	return group, nil
}

func createCanonicalConfigurationSpace(
	ctx context.Context,
	store spaces.Store,
	group syntax.DID,
	spaceType syntax.NSID,
	memberWrite bool,
) (habitat_syntax.SpaceURI, error) {
	uri, err := store.CreateSpace(ctx, group, spaceType, "self")
	if err != nil {
		return "", fmt.Errorf("create %s space: %w", spaceType, err)
	}
	policy := spaces.Policy{
		Type: spaces.PolicyAtgrp,
		Grants: []spaces.AtgrpGrant{
			{Role: atgrp.AdminRole, Read: true, Write: memberWrite},
			{Role: atgrp.MemberRole, Read: true, Write: memberWrite},
		},
	}
	if err := spaces.SetGroupSpacePolicy(ctx, store, uri, policy); err != nil {
		return "", fmt.Errorf("set %s space policy: %w", spaceType, err)
	}
	return uri, nil
}
