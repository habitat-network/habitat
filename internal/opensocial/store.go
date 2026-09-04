package opensocial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xmaps"
	"github.com/habitat-network/habitat/api/habitat"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"gorm.io/gorm"
)

const (
	AdminRoleRkey  = "admin"
	MemberRoleRkey = "member"
)

const (
	MembersSpaceType = "community.opensocial.members"
	AboutSpaceType   = "community.opensocial.about"
)

const (
	AcceptanceCollection = "community.opensocial.acceptance"
	InvitesCollection    = "community.opensocial.invites"
	MembershipCollection = "community.opensocial.membership"
	ProfileCollection    = "community.opensocial.profile"
)

// Store manages opensocial communities: their profile/role/membership repo
// records (via spacesStore) and the org-local state, such as pending
// invites, that has no repo record until a member accepts it.
type Store struct {
	db          *gorm.DB
	spacesStore spaces.Store
	blobStore   spaces.BlobStore
	hive        hive.Hive
}

func NewStore(
	db *gorm.DB,
	spacesStore spaces.Store,
	blobStore spaces.BlobStore,
	hve hive.Hive,
) (*Store, error) {
	if err := db.AutoMigrate(&inviteRow{}); err != nil {
		return nil, fmt.Errorf("automigrate: %w", err)
	}
	return &Store{
		db:          db,
		spacesStore: spacesStore,
		blobStore:   blobStore,
		hive:        hve,
	}, nil
}

func (s *Store) NewOrg(ctx context.Context, handle string, creator syntax.DID) (string, error) {
	var orgDID syntax.DID
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		orgID, err := s.hive.WithTx(tx).MintOrgIdentity(ctx, handle)
		if err != nil {
			return fmt.Errorf("mint org identity: %w", err)
		}
		spacesStoreTx := s.spacesStore.WithTx(tx)
		aboutSpace, err := spacesStoreTx.
			CreateSpace(ctx, orgID.DID, AboutSpaceType, "self")
		if err != nil {
			return fmt.Errorf("create profile space: %w", err)
		}
		recordBytes, err := spaces.MarshalRecord(opensocial_api.CommunityOpensocialProfile{
			Name:      handle,
			UpdatedAt: time.Now().Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("marshal profile record: %w", err)
		}
		if _, _, err = spacesStoreTx.PutRecord(
			ctx, aboutSpace, orgID.DID, "community.opensocial.profile", "self",
			recordBytes,
		); err != nil {
			return fmt.Errorf("put profile record: %w", err)
		}
		membersSpace, err := spacesStoreTx.CreateSpace(
			ctx, orgID.DID, MembersSpaceType, "self",
		)
		if err != nil {
			return fmt.Errorf("create members space: %w", err)
		}
		recordBytes, err = spaces.MarshalRecord(opensocial_api.CommunityOpensocialRole{
			Name:      "Admin",
			UpdatedAt: time.Now().Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("marshal role record: %w", err)
		}
		if _, _, err = spacesStoreTx.PutRecord(
			ctx, membersSpace, orgID.DID, "community.opensocial.role",
			AdminRoleRkey,
			recordBytes,
		); err != nil {
			return fmt.Errorf("put role record: %w", err)
		}
		recordBytes, err = spaces.MarshalRecord(opensocial_api.CommunityOpensocialRole{
			Name:      "Member",
			UpdatedAt: time.Now().Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("marshal role record: %w", err)
		}
		if _, _, err = spacesStoreTx.PutRecord(
			ctx, membersSpace, orgID.DID, "community.opensocial.role",
			MemberRoleRkey,
			recordBytes,
		); err != nil {
			return fmt.Errorf("put role record: %w", err)
		}
		recordBytes, err = spaces.MarshalRecord(opensocial_api.CommunityOpensocialMembership{
			Roles:     []string{AdminRoleRkey},
			UpdatedAt: time.Now().Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("marshal membership record: %w", err)
		}
		if _, _, err = spacesStoreTx.PutRecord(
			ctx, membersSpace, orgID.DID, "community.opensocial.membership",
			syntax.RecordKey(creator),
			recordBytes,
		); err != nil {
			return fmt.Errorf("put membership record: %w", err)
		}
		recordBytes, err = spaces.MarshalRecord(opensocial_api.CommunityOpensocialAccess{
			Roles:     []string{MemberRoleRkey, AdminRoleRkey},
			UpdatedAt: time.Now().Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("marshal access record: %w", err)
		}
		if _, _, err = spacesStoreTx.PutRecord(
			ctx, membersSpace, orgID.DID, "community.opensocial.access", "self",
			recordBytes,
		); err != nil {
			return fmt.Errorf("put access record: %w", err)
		}
		recordBytes, err = spaces.MarshalRecord(opensocial_api.CommunityOpensocialAccess{
			Roles:     []string{MemberRoleRkey, AdminRoleRkey},
			UpdatedAt: time.Now().Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("marshal access record: %w", err)
		}
		if _, _, err = spacesStoreTx.PutRecord(
			ctx, aboutSpace, orgID.DID, "community.opensocial.access", "self",
			recordBytes,
		); err != nil {
			return fmt.Errorf("put access record: %w", err)
		}
		orgDID = orgID.DID
		return nil
	}); err != nil {
		return "", fmt.Errorf("new org: %w", err)
	}
	return orgDID.String(), nil
}

func (s *Store) CreateSpace(
	ctx context.Context,
	orgDID syntax.DID,
	roles []string,
	spaceType syntax.NSID,
	skey habitat_syntax.SpaceKey,
) (habitat_syntax.SpaceURI, error) {
	var spaceURI habitat_syntax.SpaceURI
	var err error
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		spacesStoreTx := s.spacesStore.WithTx(tx)
		spaceURI, err = spacesStoreTx.CreateSpace(ctx, orgDID, spaceType, skey)
		if err != nil {
			return fmt.Errorf("space store create: %w", err)
		}
		recordBytes, err := spaces.MarshalRecord(opensocial_api.CommunityOpensocialAccess{
			Roles:     roles,
			UpdatedAt: time.Now().Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("marshal access record: %w", err)
		}
		if _, _, err = spacesStoreTx.PutRecord(
			ctx, spaceURI, orgDID, "community.opensocial.access", "self",
			recordBytes,
		); err != nil {
			return fmt.Errorf("put access record: %w", err)
		}
		recordBytes, err = spaces.MarshalRecord(opensocial_api.CommunityOpensocialSpace{
			Uri: spaceURI.String(),
		})
		if err != nil {
			return fmt.Errorf("marshal space record: %w", err)
		}
		if _, _, err = spacesStoreTx.PutRecord(
			ctx, spaceURI, orgDID, "community.opensocial.space", "",
			recordBytes,
		); err != nil {
			return fmt.Errorf("put space record: %w", err)
		}
		return nil
	}); err != nil {
		return "", fmt.Errorf("create space: %w", err)
	}
	return spaceURI, nil
}

func (s *Store) UploadImage(
	ctx context.Context,
	orgDID syntax.DID,
	mimeType string,
	image []byte,
) (atdata.Blob, error) {
	var blob atdata.Blob
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		spacesStoreTx := s.spacesStore.WithTx(tx)
		aboutSpace := habitat_syntax.ConstructSpaceURI(orgDID, AboutSpaceType, "self")
		existingRecord, err := spacesStoreTx.GetRecord(
			ctx, aboutSpace, orgDID, "community.opensocial.profile", "self",
		)
		if err != nil {
			return fmt.Errorf("get existing profile record: %w", err)
		}
		cid, size, err := s.blobStore.PutBlob(ctx, mimeType, image)
		if err != nil {
			return fmt.Errorf("put blob: %w", err)
		}
		blob = atdata.Blob{
			Ref:      atdata.CIDLink(cid),
			Size:     size,
			MimeType: mimeType,
		}
		existingRecord.Value["avatar"] = blob
		recordBytes, err := spaces.MarshalRecord(existingRecord.Value)
		if err != nil {
			return fmt.Errorf("marshal profile record: %w", err)
		}
		if _, _, err = spacesStoreTx.PutRecord(
			ctx, aboutSpace, orgDID, "community.opensocial.profile", "self", recordBytes,
		); err != nil {
			return fmt.Errorf("put profile record: %w", err)
		}
		return nil
	}); err != nil {
		return atdata.Blob{}, fmt.Errorf("upload image: %w", err)
	}
	return blob, nil
}

func (s *Store) UpdateProfile(
	ctx context.Context,
	orgDID syntax.DID,
	name string,
	description string,
	joinPolicy string,
) error {
	aboutSpace := habitat_syntax.ConstructSpaceURI(orgDID, AboutSpaceType, "self")
	existingRecord, err := s.spacesStore.GetRecord(
		ctx, aboutSpace, orgDID, "community.opensocial.profile", "self",
	)
	if err != nil {
		return fmt.Errorf("get existing profile record: %w", err)
	}
	existingRecord.Value["name"] = name
	if description == "" {
		delete(existingRecord.Value, "description")
	} else {
		existingRecord.Value["description"] = description
	}
	if joinPolicy != "" {
		existingRecord.Value["joinPolicy"] = joinPolicy
	}
	existingRecord.Value["updatedAt"] = time.Now().Format(time.RFC3339)
	recordBytes, err := spaces.MarshalRecord(existingRecord.Value)
	if err != nil {
		return fmt.Errorf("marshal profile record: %w", err)
	}
	if _, _, err = s.spacesStore.PutRecord(
		ctx, aboutSpace, orgDID, "community.opensocial.profile", "self", recordBytes,
	); err != nil {
		return fmt.Errorf("put profile record: %w", err)
	}
	return nil
}

func (s *Store) AssignRoles(
	ctx context.Context,
	orgDID syntax.DID,
	user syntax.DID,
	roles []string,
) error {
	recordBytes, err := spaces.MarshalRecord(opensocial_api.CommunityOpensocialMembership{
		Roles:     roles,
		UpdatedAt: time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshal membership record: %w", err)
	}
	_, _, err = s.spacesStore.PutRecord(
		ctx,
		habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
		orgDID,
		"community.opensocial.membership",
		syntax.RecordKey(user),
		recordBytes,
	)
	return err
}

func (s *Store) CheckPermission(
	ctx context.Context,
	user syntax.DID,
	space habitat_syntax.SpaceURI,
) (bool, error) {
	accessRecord, err := s.spacesStore.GetRecord(
		ctx,
		space,
		space.SpaceOwner(),
		"community.opensocial.access",
		"self",
	)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get access record: %w", err)
	}
	var access opensocial_api.CommunityOpensocialAccess
	if err := decodeRecordValue(accessRecord.Value, &access); err != nil {
		return false, fmt.Errorf("decode access record: %w", err)
	}
	membershipRoles, err := s.GetUserRoles(ctx, space.SpaceOwner(), user)
	if err != nil {
		return false, fmt.Errorf("get user roles: %w", err)
	}
	intersection := xmaps.Intersection(
		xmaps.SetFromSlice(access.Roles),
		xmaps.SetFromSlice(membershipRoles),
	)
	return len(intersection) > 0, nil
}

func (s *Store) GetUserRoles(
	ctx context.Context,
	orgDID syntax.DID,
	user syntax.DID,
) ([]string, error) {
	membershipRecord, err := s.spacesStore.GetRecord(
		ctx, habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
		orgDID,
		"community.opensocial.membership",
		syntax.RecordKey(user),
	)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get membership record: %w", err)
	}
	var membership opensocial_api.CommunityOpensocialMembership
	if err := decodeRecordValue(membershipRecord.Value, &membership); err != nil {
		return nil, fmt.Errorf("decode membership record: %w", err)
	}
	return membership.Roles, nil
}

// decodeRecordValue re-decodes a record's raw JSON value (map[string]any, as
// stored by the spaces store) into a typed lexicon record.
func decodeRecordValue(value map[string]any, out any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal record value: %w", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("unmarshal record value: %w", err)
	}
	return nil
}

func (s *Store) CheckAppAccess(
	ctx context.Context,
	spaceURI habitat_syntax.SpaceURI,
	clientID string,
) (bool, error) {
	orgDID := spaceURI.SpaceOwner()
	// any app can access the members and about spaces
	if spaceURI.SpaceType() == MembersSpaceType || spaceURI.SpaceType() == AboutSpaceType {
		return true, nil
	}
	rkey, err := habitat_syntax.AppAccessRkey(clientID)
	if err != nil {
		slog.WarnContext(ctx, "failed to get app access rkey", "err", err)
		return false, nil
	}
	_, err = s.spacesStore.GetRecord(
		ctx,
		habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
		orgDID,
		habitat_syntax.AppAccessCollection,
		rkey,
	)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get app access record: %w", err)
	}
	return true, nil
}

func (s *Store) IsOrg(ctx context.Context, orgDID syntax.DID) (bool, error) {
	exists, err := s.spacesStore.CheckSpaceExists(
		ctx,
		habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
	)
	if err != nil {
		return false, fmt.Errorf("check org space exists: %w", err)
	}
	return exists, nil
}

func (s *Store) GrantAppAccess(
	ctx context.Context,
	orgDID syntax.DID,
	clientID string,
) error {
	rkey, err := habitat_syntax.AppAccessRkey(clientID)
	if err != nil {
		return fmt.Errorf("construct app access rkey: %w", err)
	}
	recordBytes, err := spaces.MarshalRecord(habitat.NetworkHabitatSpaceAppAccess{
		CreatedAt: time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshal app access record: %w", err)
	}
	_, _, err = s.spacesStore.PutRecord(
		ctx,
		habitat_syntax.ConstructSpaceURI(
			orgDID,
			MembersSpaceType,
			"self",
		),
		orgDID,
		habitat_syntax.AppAccessCollection,
		rkey,
		recordBytes,
	)
	if err != nil {
		return fmt.Errorf("put app access record: %w", err)
	}
	return nil
}

func (s *Store) GetProfile(
	ctx context.Context,
	orgDID syntax.DID,
) (opensocial_api.CommunityOpensocialProfile, error) {
	profileRecord, err := s.spacesStore.GetRecord(
		ctx,
		habitat_syntax.ConstructSpaceURI(orgDID, AboutSpaceType, "self"),
		orgDID,
		ProfileCollection,
		"self",
	)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return opensocial_api.CommunityOpensocialProfile{}, nil
	}
	if err != nil {
		return opensocial_api.CommunityOpensocialProfile{}, fmt.Errorf(
			"get profile record: %w",
			err,
		)
	}
	var profile opensocial_api.CommunityOpensocialProfile
	if err := decodeRecordValue(profileRecord.Value, &profile); err != nil {
		return opensocial_api.CommunityOpensocialProfile{}, fmt.Errorf(
			"decode profile record: %w",
			err,
		)
	}
	return profile, nil
}
