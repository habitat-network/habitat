package org

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/tuple"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/fgastore"
)

// orgRoleRelation maps an org Role to its FGA relation.
func orgRoleRelation(role Role) string {
	if role == AdminRole {
		return fgastore.RelationAdmin
	}
	return fgastore.RelationMember
}

// setOrgRoleFGA mirrors a member's role into FGA: it writes the tuple for the
// member's current role and removes the tuple for the opposite role. Both
// operations are idempotent. A nil fga store is a no-op so non-FGA-backed
// callers (e.g. tests) work unchanged.
func setOrgRoleFGA(
	ctx context.Context,
	fga fgastore.Store,
	orgID, did syntax.DID,
	role Role,
) error {
	if fga == nil {
		return nil
	}
	obj := fgastore.OrgObjectKey(orgID)
	user := fgastore.MemberUserString(did)
	write := orgRoleRelation(role)
	remove := fgastore.RelationMember
	if write == fgastore.RelationMember {
		remove = fgastore.RelationAdmin
	}
	if err := fga.WriteRaw(ctx, &openfgav1.WriteRequest{
		Writes: &openfgav1.WriteRequestWrites{
			TupleKeys:   []*openfgav1.TupleKey{tuple.NewTupleKey(obj, write, user)},
			OnDuplicate: "ignore",
		},
		Deletes: &openfgav1.WriteRequestDeletes{
			TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
				tuple.TupleKeyToTupleKeyWithoutCondition(tuple.NewTupleKey(obj, remove, user)),
			},
			OnMissing: "ignore",
		},
	}); err != nil {
		return fmt.Errorf("sync org role to fga: %w", err)
	}
	return nil
}

// removeOrgRoleFGA removes both role tuples for a DID in an org, idempotently.
func removeOrgRoleFGA(
	ctx context.Context,
	fga fgastore.Store,
	orgID, did syntax.DID,
) error {
	if fga == nil {
		return nil
	}
	obj := fgastore.OrgObjectKey(orgID)
	user := fgastore.MemberUserString(did)
	if err := fga.WriteRaw(ctx, &openfgav1.WriteRequest{
		Deletes: &openfgav1.WriteRequestDeletes{
			TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
				tuple.TupleKeyToTupleKeyWithoutCondition(
					tuple.NewTupleKey(obj, fgastore.RelationAdmin, user),
				),
				tuple.TupleKeyToTupleKeyWithoutCondition(
					tuple.NewTupleKey(obj, fgastore.RelationMember, user),
				),
			},
			OnMissing: "ignore",
		},
	}); err != nil {
		return fmt.Errorf("remove org role from fga: %w", err)
	}
	return nil
}

// backfillFGA mirrors every existing member's org role into FGA. It runs once at
// startup so orgs created before FGA-backed membership still resolve. Idempotent.
func backfillFGA(ctx context.Context, db *gorm.DB, fga fgastore.Store) error {
	if fga == nil {
		return nil
	}
	var members []member
	if err := db.WithContext(ctx).Find(&members).Error; err != nil {
		return fmt.Errorf("backfill fga: list members: %w", err)
	}
	for _, m := range members {
		if err := setOrgRoleFGA(ctx, fga, m.OrgID, m.Did, m.Role); err != nil {
			return err
		}
	}
	return nil
}
