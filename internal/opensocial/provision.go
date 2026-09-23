package opensocial

import (
	"context"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"gorm.io/gorm"
)

// ProvisionMember adds memberDID to orgDID by writing its membership record
// (authored by the org) and its own acceptance record (authored by the
// member) in a single transaction. Unlike RequestJoin, no prior invite is
// required — used when the caller has independent authority to enroll the
// member (e.g. the email-domain auto-provisioning flow, see
// identity.EmailResolver) and there's no invitee session to author the
// acceptance record under, so the backend authors both records directly.
//
// memberDID is granted the admin role if it is the org's first member
// (i.e. the org was created via NewOrgWithoutCreator and nobody has joined
// yet), and the member role otherwise. This check-and-write is made
// race-safe by acquiring the same per-(space, repo) advisory lock PutRecord
// itself takes before counting existing community.opensocial.membership
// records for orgDID, so two concurrent first sign-ins cannot both become
// admin.
func (s *Store) ProvisionMember(ctx context.Context, orgDID, memberDID syntax.DID) error {
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		spacesStoreTx := s.spacesStore.WithTx(tx)
		membersSpace := habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self")
		if err := spacesStoreTx.LockRepo(ctx, membersSpace, orgDID); err != nil {
			return fmt.Errorf("lock members repo: %w", err)
		}
		membershipNSID := syntax.NSID(MembershipCollection)
		existing, err := spacesStoreTx.ListRecords(ctx, membersSpace, orgDID, &membershipNSID)
		if err != nil {
			return fmt.Errorf("list memberships: %w", err)
		}
		roles := []string{MemberRoleRkey}
		if len(existing) == 0 {
			roles = []string{AdminRoleRkey}
		}
		if err := putMembership(ctx, spacesStoreTx, orgDID, memberDID, roles); err != nil {
			return err
		}
		recordBytes, err := spaces.MarshalRecord(opensocial_api.CommunityOpensocialAcceptance{
			UpdatedAt: time.Now().Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("marshal acceptance record: %w", err)
		}
		// repo only selects whose record namespace this lands in; it isn't a
		// credential check, so the backend can author it under memberDID.
		if _, _, err := spacesStoreTx.PutRecord(
			ctx, membersSpace, memberDID, AcceptanceCollection, "self", recordBytes,
		); err != nil {
			return fmt.Errorf("put acceptance record: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("provision member: %w", err)
	}
	return nil
}
