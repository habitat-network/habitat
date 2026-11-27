package spaces

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/api/atgrp"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// AtgrpAccess returns the independent personal read and write grants for the
// caller's current role IDs. Public visibility grants read, not write.
// Callers must handle the implicit authority grant separately.
func (p Policy) AtgrpAccess(roles []string) (read, write bool) {
	if p.Type != PolicyAtgrp {
		return false, false
	}
	read = p.PublicRead
	for _, grant := range p.Grants {
		if slices.Contains(roles, grant.Role) {
			read = read || grant.Read
			write = write || grant.Write
		}
	}
	return read, write
}

// SyncGroupSpaceIndex inserts or removes the host's index record based on the
// space's current state. Call it inside the same group transaction that creates,
// updates or deletes a modality. The index is not a source of authorization.
func SyncGroupSpaceIndex(ctx context.Context, store Store, uri habitat_syntax.SpaceURI) error {
	return store.syncGroupSpaceIndex(ctx, uri)
}

func (s *store) syncGroupSpaceIndex(ctx context.Context, uri habitat_syntax.SpaceURI) error {
	if habitat_syntax.IsAtgrpConfigurationSpaceType(uri.SpaceType()) {
		return nil
	}
	group := uri.SpaceOwner()
	members := habitat_syntax.ConstructSpaceURI(group, habitat_syntax.AtgrpMembersSpaceType, "self")
	return Transaction(ctx, s.db, func(tx *gorm.DB) error {
		store := s.withTx(tx)
		if err := lockRepo(tx, members, group); err != nil {
			return err
		}
		membersPolicy, err := store.GetSpacePolicy(ctx, members)
		if err != nil {
			return err
		}
		if membersPolicy.Type != PolicyAtgrp {
			return fmt.Errorf("%w: members space must use the group policy", ErrUnsupportedPolicy)
		}
		var row space
		listed := false
		err = store.spacePolicyRow(ctx, uri).First(&row).Error
		if err == nil {
			policy, err := ParsePolicy(row.Policy)
			if err != nil {
				return err
			}
			if policy.Type != PolicyAtgrp {
				return fmt.Errorf(
					"%w: indexed space must use the group policy",
					ErrUnsupportedPolicy,
				)
			}
			listed = !policy.Unlisted
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		ref := uri.AtSpace().String()
		var keys []syntax.RecordKey
		err = tx.Model(&spaceRecord{}).Scopes(habitat_syntax.InSpace(members)).
			Where("repo = ? AND collection = ?", group, habitat_syntax.AtgrpSpaceCollection).
			Where(datatypes.JSONQuery("value_json").Equals(ref, "space")).
			Order("rkey").Pluck("rkey", &keys).Error
		if err != nil {
			return fmt.Errorf("find group space index entry: %w", err)
		}
		if listed && len(keys) == 0 {
			value, err := MarshalRecord(atgrp.OrgAtgrpSpace{
				Space: ref, CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339Nano),
			})
			if err != nil {
				return err
			}
			_, _, err = store.PutRecord(
				ctx,
				members,
				group,
				habitat_syntax.AtgrpSpaceCollection,
				"",
				value,
			)
			return err
		}
		if listed {
			keys = keys[1:]
		}
		for _, key := range keys {
			if err := store.DeleteRecord(
				ctx,
				members,
				group,
				habitat_syntax.AtgrpSpaceCollection,
				key.String(),
			); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReconcileGroupSpaceWriters removes registered writers that no longer have a
// write grant. Resolve roles against the same transaction as the policy change.
// Leaving publicRead set does not preserve anyone's write grant.
func ReconcileGroupSpaceWriters(
	ctx context.Context, store Store, uri habitat_syntax.SpaceURI,
	roles func(context.Context, syntax.DID) ([]string, error),
) error {
	policy, err := store.GetSpacePolicy(ctx, uri)
	if err != nil {
		return err
	}
	if policy.Type != PolicyAtgrp {
		return fmt.Errorf("%w: expected a group policy", ErrUnsupportedPolicy)
	}
	writers, err := store.ListRepos(ctx, uri)
	if err != nil {
		return err
	}
	for _, writer := range writers {
		if writer.DID == uri.SpaceOwner() {
			continue
		}
		current, err := roles(ctx, writer.DID)
		if err != nil {
			return err
		}
		if _, write := policy.AtgrpAccess(current); !write {
			if err := store.RemoveWriter(ctx, uri, writer.DID); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReconcileGroupWriter removes one writer from any space in which their
// remaining roles do not grant write. It inspects the actual writer set,
// including in unlisted spaces. Call from the membership write transaction.
func ReconcileGroupWriter(
	ctx context.Context, store Store, group, writer syntax.DID, remainingRoles []string,
) error {
	if writer == group {
		return nil
	}
	spaces, err := store.listGroupWriterSpaces(ctx, group, writer)
	if err != nil {
		return err
	}
	for _, space := range spaces {
		policy, err := ParsePolicy(space.Policy)
		if err != nil {
			return err
		}
		if policy.Type != PolicyAtgrp {
			continue
		}
		if _, write := policy.AtgrpAccess(remainingRoles); !write {
			if err := store.RemoveWriter(ctx, space.URI(), writer); err != nil {
				return err
			}
		}
	}
	return nil
}

type groupWriterSpace struct {
	habitat_syntax.SpaceRef `       gorm:"embedded;embeddedPrefix:space_"`
	Policy                  []byte
}

func (s *store) listGroupWriterSpaces(
	ctx context.Context,
	group, writer syntax.DID,
) ([]groupWriterSpace, error) {
	var rows []groupWriterSpace
	err := s.db.WithContext(ctx).Table("space_repos AS r").
		Select("r.space_owner, r.space_type, r.space_skey, s.policy").
		Joins("JOIN spaces AS s ON r.space_owner = s.owner AND r.space_type = s.type AND r.space_skey = s.skey").
		Where("r.space_owner = ? AND r.repo = ? AND r.deleted_at IS NULL", group, writer).
		Where("r.removed IS NULL OR r.removed = ?", false).
		Order("r.space_type, r.space_skey").
		Scan(&rows).Error
	return rows, err
}
