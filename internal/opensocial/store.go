package opensocial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ipfs/go-cid"
	"log/slog"
	"time"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xmaps"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/api/habitat"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/telemetry"
)

const (
	AdminRoleRkey  = "admin"
	MemberRoleRkey = "member"
)

const (
	MembersSpaceType = "group.opensocial.members"
	InvitesSpaceType = "group.opensocial.invites"
	MetaSpaceType    = "group.opensocial.meta"
)

const (
	AcceptanceCollection    = "group.opensocial.acceptance"
	InvitesCollection       = "group.opensocial.invites"
	MembershipCollection    = "group.opensocial.membership"
	ProfileCollection       = "group.opensocial.profile"
	MemberProfileCollection = "network.habitat.opensocial.memberProfile"
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
	return &Store{
		db:          db,
		spacesStore: spacesStore,
		blobStore:   blobStore,
		hive:        hve,
	}, nil
}

func (s *Store) NewOrg(
	ctx context.Context,
	handle string,
	creator syntax.DID,
) (_ string, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.NewOrg", trace.WithAttributes(
		attribute.String("habitat.opensocial.handle", handle), memberAttr(creator),
	))
	defer func() { telemetry.End(span, err) }()
	var orgDID syntax.DID
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		orgID, err := s.createOrgShell(ctx, tx, handle)
		if err != nil {
			return err
		}
		if err := putMembership(
			ctx, s.spacesStore.WithTx(tx), orgID.DID, creator,
			[]string{MemberRoleRkey, AdminRoleRkey},
		); err != nil {
			return err
		}
		orgDID = orgID.DID
		return nil
	}); err != nil {
		return "", fmt.Errorf("new org: %w", err)
	}
	return orgDID.String(), nil
}

// NewOrgWithoutCreator creates an org's spaces, built-in roles, access, and
// permissions records exactly like NewOrg, but writes no initial membership
// — used when the org will be populated by domain-based auto-provisioning
// (see ProvisionMember) rather than by an existing DID creating it directly.
func (s *Store) NewOrgWithoutCreator(ctx context.Context, handle string) (_ string, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.NewOrgWithoutCreator", trace.WithAttributes(
		attribute.String("habitat.opensocial.handle", handle),
	))
	defer func() { telemetry.End(span, err) }()
	var orgDID syntax.DID
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		orgID, err := s.createOrgShell(ctx, tx, handle)
		if err != nil {
			return err
		}
		orgDID = orgID.DID
		return nil
	}); err != nil {
		return "", fmt.Errorf("new org without creator: %w", err)
	}
	return orgDID.String(), nil
}

// WithTx returns a copy of the store whose writes run on tx, so callers can
// compose its operations with other stores' writes in one transaction.
func (s *Store) WithTx(tx *gorm.DB) *Store {
	return &Store{
		db:          tx,
		spacesStore: s.spacesStore.WithTx(tx),
		blobStore:   s.blobStore,
		hive:        s.hive.WithTx(tx),
	}
}

// createOrgShell mints the org identity and writes its about/members spaces,
// profile, built-in roles, access, and permissions records within tx — every
// part of org creation except its initial membership.
func (s *Store) createOrgShell(
	ctx context.Context,
	tx *gorm.DB,
	handle string,
) (*identity.Identity, error) {
	orgID, err := s.hive.WithTx(tx).MintOrgIdentity(ctx, handle)
	if err != nil {
		return nil, fmt.Errorf("mint org identity: %w", err)
	}
	spacesStoreTx := s.spacesStore.WithTx(tx)
	metaSpace, err := spacesStoreTx.
		CreateSpace(ctx, orgID.DID, MetaSpaceType, "self")
	if err != nil {
		return nil, fmt.Errorf("create profile space: %w", err)
	}
	recordBytes, err := spaces.MarshalRecord(opensocial_api.GroupOpensocialProfile{
		DisplayName: handle,
		// Orgs have always been invite-only.
		JoinPolicy: "invite",
	})
	if err != nil {
		return nil, fmt.Errorf("marshal profile record: %w", err)
	}
	if _, _, err = spacesStoreTx.PutRecord(
		ctx, metaSpace, orgID.DID, "group.opensocial.profile", "self",
		recordBytes,
	); err != nil {
		return nil, fmt.Errorf("put profile record: %w", err)
	}
	membersSpace, err := spacesStoreTx.CreateSpace(
		ctx, orgID.DID, MembersSpaceType, "self",
	)
	if err != nil {
		return nil, fmt.Errorf("create members space: %w", err)
	}
	recordBytes, err = spaces.MarshalRecord(opensocial_api.GroupOpensocialRole{
		DisplayName: "Admin",
	})
	if err != nil {
		return nil, fmt.Errorf("marshal role record: %w", err)
	}
	if _, _, err = spacesStoreTx.PutRecord(
		ctx, membersSpace, orgID.DID, "group.opensocial.role",
		AdminRoleRkey,
		recordBytes,
	); err != nil {
		return nil, fmt.Errorf("put role record: %w", err)
	}
	recordBytes, err = spaces.MarshalRecord(opensocial_api.GroupOpensocialRole{
		DisplayName: "Member",
		// Members admitted through requestJoin get this role.
		Default: true,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal role record: %w", err)
	}
	if _, _, err = spacesStoreTx.PutRecord(
		ctx, membersSpace, orgID.DID, "group.opensocial.role",
		MemberRoleRkey,
		recordBytes,
	); err != nil {
		return nil, fmt.Errorf("put role record: %w", err)
	}
	recordBytes, err = spaces.MarshalRecord(opensocial_api.GroupOpensocialAccess{
		Grants: readGrants([]string{MemberRoleRkey, AdminRoleRkey}),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal access record: %w", err)
	}
	if _, _, err = spacesStoreTx.PutRecord(
		ctx, membersSpace, orgID.DID, "group.opensocial.access", "self",
		recordBytes,
	); err != nil {
		return nil, fmt.Errorf("put access record: %w", err)
	}
	recordBytes, err = spaces.MarshalRecord(opensocial_api.GroupOpensocialAccess{
		// The meta space is the group's public face.
		PublicRead: true,
		Grants:     readGrants([]string{MemberRoleRkey, AdminRoleRkey}),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal access record: %w", err)
	}
	if _, _, err = spacesStoreTx.PutRecord(
		ctx, metaSpace, orgID.DID, "group.opensocial.access", "self",
		recordBytes,
	); err != nil {
		return nil, fmt.Errorf("put access record: %w", err)
	}
	// The admin role starts out holding every scope and able to assign either
	// built-in role; the member role holds what a member needs to take part.
	// Groups can change both via the permissions record once they hold the
	// configure permission set.
	permissionsBytes, err := spaces.MarshalRecord(opensocial_api.GroupOpensocialPermissions{
		Grants: bootstrapGrants(),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal permissions record: %w", err)
	}
	if _, _, err = spacesStoreTx.PutRecord(
		ctx, membersSpace, orgID.DID, PermissionsCollection, "self",
		permissionsBytes,
	); err != nil {
		return nil, fmt.Errorf("put permissions record: %w", err)
	}
	return orgID, nil
}

// putMembership writes member's group.opensocial.membership record,
// authored by the org, into the org's members space.
func putMembership(
	ctx context.Context,
	spacesStore spaces.Store,
	orgDID, member syntax.DID,
	roles []string,
) error {
	_, err := putMembershipRef(ctx, spacesStore, orgDID, member, roles)
	return err
}

// putMembershipRef is putMembership returning a reference to the record.
func putMembershipRef(
	ctx context.Context,
	spacesStore spaces.Store,
	orgDID, member syntax.DID,
	roles []string,
) (StrongRef, error) {
	recordBytes, err := spaces.MarshalRecord(opensocial_api.GroupOpensocialMembership{
		Roles:     roles,
		CreatedAt: time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return StrongRef{}, fmt.Errorf("marshal membership record: %w", err)
	}
	uri, c, err := spacesStore.PutRecord(
		ctx,
		habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
		orgDID,
		MembershipCollection,
		syntax.RecordKey(member),
		recordBytes,
	)
	if err != nil {
		return StrongRef{}, fmt.Errorf("put membership record: %w", err)
	}
	return strongRef(uri, c), nil
}

func (s *Store) CreateSpace(
	ctx context.Context,
	orgDID syntax.DID,
	roles []string,
	spaceType syntax.NSID,
	skey habitat_syntax.SpaceKey,
	policy spaces.Policy,
) (_ habitat_syntax.SpaceURI, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.CreateSpace", trace.WithAttributes(
		telemetry.Org(orgDID),
		rolesKey.StringSlice(roles),
		attribute.String("habitat.space.type", spaceType.String()),
	))
	defer func() { telemetry.End(span, err) }()
	created, err := s.createSpace(
		ctx,
		orgDID,
		spaceType,
		skey,
		policy,
		opensocial_api.GroupOpensocialAccess{
			Grants: readGrants(roles),
		},
		true,
	)
	if err != nil {
		return "", err
	}
	return created.Space, nil
}

// createdSpace is a space written by createSpace, with the access and index
// records that describe it.
type createdSpace struct {
	Space  habitat_syntax.SpaceURI
	Access StrongRef
	// Index is nil when the space was not added to the group's index.
	Index *StrongRef
}

// StrongRef names a record at a particular version.
type StrongRef struct {
	URI string
	CID string
}

func (s *Store) createSpace(
	ctx context.Context,
	orgDID syntax.DID,
	spaceType syntax.NSID,
	skey habitat_syntax.SpaceKey,
	policy spaces.Policy,
	access opensocial_api.GroupOpensocialAccess,
	index bool,
) (createdSpace, error) {
	var created createdSpace
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		spacesStoreTx := s.spacesStore.WithTx(tx)
		spaceURI, err := spacesStoreTx.CreateSpace(ctx, orgDID, spaceType, skey)
		if err != nil {
			return fmt.Errorf("space store create: %w", err)
		}
		created.Space = spaceURI
		if err := spacesStoreTx.SetSpacePolicy(ctx, spaceURI, policy); err != nil {
			return fmt.Errorf("set space policy: %w", err)
		}
		recordBytes, err := spaces.MarshalRecord(access)
		if err != nil {
			return fmt.Errorf("marshal access record: %w", err)
		}
		accessURI, accessCID, err := spacesStoreTx.PutRecord(
			ctx, spaceURI, orgDID, "group.opensocial.access", "self",
			recordBytes,
		)
		if err != nil {
			return fmt.Errorf("put access record: %w", err)
		}
		created.Access = strongRef(accessURI, accessCID)
		if !index {
			return nil
		}
		// New records name the space in the at:space/ form clients now write;
		// older ones keep the at:// form until the URI migration.
		recordBytes, err = spaces.MarshalRecord(opensocial_api.GroupOpensocialSpace{
			Space:     spaceURI.AtSpace().String(),
			CreatedAt: time.Now().Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("marshal space record: %w", err)
		}
		indexURI, indexCID, err := spacesStoreTx.PutRecord(
			ctx,
			habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
			orgDID, "group.opensocial.space", "",
			recordBytes,
		)
		if err != nil {
			return fmt.Errorf("put space record: %w", err)
		}
		ref := strongRef(indexURI, indexCID)
		created.Index = &ref
		return nil
	}); err != nil {
		return createdSpace{}, fmt.Errorf("create space: %w", err)
	}
	return created, nil
}

func strongRef(uri habitat_syntax.SpaceRecordURI, c *cid.Cid) StrongRef {
	ref := StrongRef{URI: string(uri)}
	if c != nil {
		ref.CID = c.String()
	}
	return ref
}

// UpdateSpace replaces the roles that may read a space by rewriting its
// group.opensocial.access record.
func (s *Store) UpdateSpace(
	ctx context.Context,
	spaceURI habitat_syntax.SpaceURI,
	roles []string,
) (err error) {
	ctx, span := tracer.Start(ctx, "opensocial.UpdateSpace", trace.WithAttributes(
		telemetry.Space(spaceURI), rolesKey.StringSlice(roles),
	))
	defer func() { telemetry.End(span, err) }()
	access, err := s.getAccess(ctx, spaceURI)
	if err != nil {
		return err
	}
	access.Grants = withReadRoles(access.Grants, roles)
	recordBytes, err := spaces.MarshalRecord(access)
	if err != nil {
		return fmt.Errorf("marshal access record: %w", err)
	}
	if _, _, err := s.spacesStore.PutRecord(
		ctx, spaceURI, spaceURI.SpaceOwner(), "group.opensocial.access", "self",
		recordBytes,
	); err != nil {
		return fmt.Errorf("put access record: %w", err)
	}
	return nil
}

// getAccess returns spaceURI's access record, or a zero value if it has none.
func (s *Store) getAccess(
	ctx context.Context,
	spaceURI habitat_syntax.SpaceURI,
) (opensocial_api.GroupOpensocialAccess, error) {
	record, err := s.spacesStore.GetRecord(
		ctx, spaceURI, spaceURI.SpaceOwner(), "group.opensocial.access", "self",
	)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return opensocial_api.GroupOpensocialAccess{}, nil
	}
	if err != nil {
		return opensocial_api.GroupOpensocialAccess{}, fmt.Errorf("get access record: %w", err)
	}
	var access opensocial_api.GroupOpensocialAccess
	if err := decodeRecordValue(record.Value, &access); err != nil {
		return opensocial_api.GroupOpensocialAccess{}, fmt.Errorf("decode access record: %w", err)
	}
	return access, nil
}

func (s *Store) UploadImage(
	ctx context.Context,
	orgDID syntax.DID,
	mimeType string,
	image []byte,
) (_ atdata.Blob, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.UploadImage", trace.WithAttributes(
		telemetry.Org(
			orgDID,
		),
		attribute.String("habitat.blob.mime_type", mimeType),
		attribute.Int("habitat.blob.size", len(image)),
	))
	defer func() { telemetry.End(span, err) }()
	var blob atdata.Blob
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		spacesStoreTx := s.spacesStore.WithTx(tx)
		metaSpace := habitat_syntax.ConstructSpaceURI(orgDID, MetaSpaceType, "self")
		existingRecord, err := spacesStoreTx.GetRecord(
			ctx, metaSpace, orgDID, "group.opensocial.profile", "self",
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
			ctx, metaSpace, orgDID, "group.opensocial.profile", "self", recordBytes,
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
) (err error) {
	ctx, span := tracer.Start(ctx, "opensocial.UpdateProfile", trace.WithAttributes(
		telemetry.Org(orgDID),
	))
	defer func() { telemetry.End(span, err) }()
	metaSpace := habitat_syntax.ConstructSpaceURI(orgDID, MetaSpaceType, "self")
	existingRecord, err := s.spacesStore.GetRecord(
		ctx, metaSpace, orgDID, "group.opensocial.profile", "self",
	)
	if err != nil {
		return fmt.Errorf("get existing profile record: %w", err)
	}
	existingRecord.Value["displayName"] = name
	if description == "" {
		delete(existingRecord.Value, "description")
	} else {
		existingRecord.Value["description"] = description
	}
	if joinPolicy != "" {
		existingRecord.Value["joinPolicy"] = joinPolicy
	}
	recordBytes, err := spaces.MarshalRecord(existingRecord.Value)
	if err != nil {
		return fmt.Errorf("marshal profile record: %w", err)
	}
	if _, _, err = s.spacesStore.PutRecord(
		ctx, metaSpace, orgDID, "group.opensocial.profile", "self", recordBytes,
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
) (err error) {
	ctx, span := tracer.Start(ctx, "opensocial.AssignRoles", trace.WithAttributes(
		telemetry.Org(orgDID), memberAttr(user), rolesKey.StringSlice(roles),
	))
	defer func() { telemetry.End(span, err) }()
	recordBytes, err := spaces.MarshalRecord(opensocial_api.GroupOpensocialMembership{
		Roles:     roles,
		CreatedAt: time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshal membership record: %w", err)
	}
	_, _, err = s.spacesStore.PutRecord(
		ctx,
		habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
		orgDID,
		"group.opensocial.membership",
		syntax.RecordKey(user),
		recordBytes,
	)
	if err != nil {
		return err
	}
	// Writing a membership approves any pending request to join.
	return s.clearJoinRequest(ctx, orgDID, user)
}

func (s *Store) CheckPermission(
	ctx context.Context,
	user syntax.DID,
	space habitat_syntax.SpaceURI,
) (ok bool, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.CheckPermission", trace.WithAttributes(
		memberAttr(user), telemetry.Space(space),
	))
	defer func() {
		span.SetAttributes(allowedKey.Bool(ok))
		telemetry.End(span, err)
	}()
	// The group DID is implicitly a member that can read every group space.
	if user == space.SpaceOwner() {
		return true, nil
	}
	accessRecord, err := s.spacesStore.GetRecord(
		ctx,
		space,
		space.SpaceOwner(),
		"group.opensocial.access",
		"self",
	)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get access record: %w", err)
	}
	var access opensocial_api.GroupOpensocialAccess
	if err := decodeRecordValue(accessRecord.Value, &access); err != nil {
		return false, fmt.Errorf("decode access record: %w", err)
	}
	if access.PublicRead {
		return true, nil
	}
	membershipRoles, err := s.GetUserRoles(ctx, space.SpaceOwner(), user)
	if err != nil {
		return false, fmt.Errorf("get user roles: %w", err)
	}
	// Every member can reach the members space whatever their roles, since
	// they write their own acceptance and profile records there. Its access
	// record only lists the built-in roles, so a member holding only a custom
	// role would otherwise be refused (a 401) when accepting an invite.
	if space.SpaceType() == MembersSpaceType {
		return len(membershipRoles) > 0, nil
	}
	intersection := xmaps.Intersection(
		xmaps.SetFromSlice(ReadRoles(access.Grants)),
		xmaps.SetFromSlice(membershipRoles),
	)
	return len(intersection) > 0, nil
}

// ListMemberSpaces returns the group.opensocial.members spaces `user`
// belongs to — every org whose members space it holds a permissioned repo
// in (i.e. has written its own record into, such as a
// group.opensocial.acceptance record when joining).
func (s *Store) ListMemberSpaces(
	ctx context.Context,
	user syntax.DID,
) (_ []habitat_syntax.SpaceURI, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.ListMemberSpaces", trace.WithAttributes(
		memberAttr(user),
	))
	defer func() { telemetry.End(span, err) }()
	membersType := syntax.NSID(MembersSpaceType)
	return s.spacesStore.ListSpaces(ctx, user, nil, &membersType)
}

func (s *Store) GetUserRoles(
	ctx context.Context,
	orgDID syntax.DID,
	user syntax.DID,
) (roles []string, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.GetUserRoles", trace.WithAttributes(
		telemetry.Org(orgDID), memberAttr(user),
	))
	defer func() {
		span.SetAttributes(rolesKey.StringSlice(roles))
		telemetry.End(span, err)
	}()
	membershipRecord, err := s.spacesStore.GetRecord(
		ctx, habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
		orgDID,
		"group.opensocial.membership",
		syntax.RecordKey(user),
	)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get membership record: %w", err)
	}
	var membership opensocial_api.GroupOpensocialMembership
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
) (ok bool, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.CheckAppAccess", trace.WithAttributes(
		telemetry.Space(spaceURI), clientIDKey.String(clientID),
	))
	defer func() {
		span.SetAttributes(allowedKey.Bool(ok))
		telemetry.End(span, err)
	}()
	orgDID := spaceURI.SpaceOwner()
	// any app can access the members and about spaces
	if spaceURI.SpaceType() == MembersSpaceType || spaceURI.SpaceType() == MetaSpaceType {
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

func (s *Store) IsOrg(ctx context.Context, orgDID syntax.DID) (ok bool, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.IsOrg", trace.WithAttributes(
		telemetry.Org(orgDID),
	))
	defer func() {
		span.SetAttributes(allowedKey.Bool(ok))
		telemetry.End(span, err)
	}()
	exists, err := s.spacesStore.CheckSpaceExists(
		ctx,
		habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
	)
	if err != nil {
		return false, fmt.Errorf("check org space exists: %w", err)
	}
	return exists, nil
}

// GrantAppAccess grants clientID access to orgDID's members space (and so,
// via CheckAppAccess, every space of the org), overwriting any prior grant.
// scopes records what was actually approved as of this grant — e.g. when
// clientID is approved to act as the org's own DID via an org credential —
// for display; it isn't itself enforced.
func (s *Store) GrantAppAccess(
	ctx context.Context,
	orgDID syntax.DID,
	clientID string,
	scopes []string,
) (err error) {
	ctx, span := tracer.Start(ctx, "opensocial.GrantAppAccess", trace.WithAttributes(
		telemetry.Org(orgDID), clientIDKey.String(clientID),
	))
	defer func() { telemetry.End(span, err) }()
	rkey, err := habitat_syntax.AppAccessRkey(clientID)
	if err != nil {
		return fmt.Errorf("construct app access rkey: %w", err)
	}
	recordBytes, err := spaces.MarshalRecord(habitat.NetworkHabitatSpaceAppAccess{
		CreatedAt: time.Now().Format(time.RFC3339),
		Scopes:    scopes,
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
) (_ opensocial_api.GroupOpensocialProfile, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.GetProfile", trace.WithAttributes(
		telemetry.Org(orgDID),
	))
	defer func() { telemetry.End(span, err) }()
	profileRecord, err := s.spacesStore.GetRecord(
		ctx,
		habitat_syntax.ConstructSpaceURI(orgDID, MetaSpaceType, "self"),
		orgDID,
		ProfileCollection,
		"self",
	)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return opensocial_api.GroupOpensocialProfile{}, nil
	}
	if err != nil {
		return opensocial_api.GroupOpensocialProfile{}, fmt.Errorf(
			"get profile record: %w",
			err,
		)
	}
	var profile opensocial_api.GroupOpensocialProfile
	if err := decodeRecordValue(profileRecord.Value, &profile); err != nil {
		return opensocial_api.GroupOpensocialProfile{}, fmt.Errorf(
			"decode profile record: %w",
			err,
		)
	}
	return profile, nil
}

// GetMemberProfile returns memberDID's network.habitat.opensocial.memberProfile
// record within orgDID's members space. Returns the zero value, no error, if
// the member hasn't set a profile.
func (s *Store) GetMemberProfile(
	ctx context.Context,
	orgDID, memberDID syntax.DID,
) (_ habitat.NetworkHabitatOpensocialMemberProfile, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.GetMemberProfile", trace.WithAttributes(
		telemetry.Org(orgDID), memberAttr(memberDID),
	))
	defer func() { telemetry.End(span, err) }()
	profileRecord, err := s.spacesStore.GetRecord(
		ctx,
		habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
		memberDID,
		MemberProfileCollection,
		syntax.RecordKey(memberDID),
	)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return habitat.NetworkHabitatOpensocialMemberProfile{}, nil
	}
	if err != nil {
		return habitat.NetworkHabitatOpensocialMemberProfile{}, fmt.Errorf(
			"get member profile record: %w",
			err,
		)
	}
	var profile habitat.NetworkHabitatOpensocialMemberProfile
	if err := decodeRecordValue(profileRecord.Value, &profile); err != nil {
		return habitat.NetworkHabitatOpensocialMemberProfile{}, fmt.Errorf(
			"decode member profile record: %w",
			err,
		)
	}
	return profile, nil
}

// GetMemberProfiles batch-fetches the network.habitat.opensocial.memberProfile
// records for memberDIDs within orgDID's members space, in a single query
// (see spaces.Store.GetRecords), keyed by DID. Members with no profile record
// are simply absent from the result.
func (s *Store) GetMemberProfiles(
	ctx context.Context,
	orgDID syntax.DID,
	memberDIDs []syntax.DID,
) (_ map[syntax.DID]habitat.NetworkHabitatOpensocialMemberProfile, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.GetMemberProfiles", trace.WithAttributes(
		telemetry.Org(orgDID), attribute.Int("habitat.opensocial.members", len(memberDIDs)),
	))
	defer func() { telemetry.End(span, err) }()
	membersSpace := habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self")
	uris := make([]habitat_syntax.SpaceRecordURI, len(memberDIDs))
	for i, memberDID := range memberDIDs {
		uris[i] = habitat_syntax.ConstructSpaceRecordURI(
			membersSpace, memberDID, MemberProfileCollection, syntax.RecordKey(memberDID),
		)
	}
	records, err := s.spacesStore.GetRecords(ctx, uris)
	if err != nil {
		return nil, fmt.Errorf("get member profile records: %w", err)
	}
	profiles := make(
		map[syntax.DID]habitat.NetworkHabitatOpensocialMemberProfile,
		len(records),
	)
	for _, record := range records {
		var profile habitat.NetworkHabitatOpensocialMemberProfile
		if err := decodeRecordValue(record.Value, &profile); err != nil {
			return nil, fmt.Errorf("decode member profile record for %s: %w", record.Owner, err)
		}
		profiles[record.Owner] = profile
	}
	return profiles, nil
}

// SeedMemberProfile writes memberDID's initial network.habitat.opensocial.memberProfile
// record within orgDID's members space, authored by the member, if one
// doesn't already exist. Used to pre-fill a profile from login-provider
// account metadata (e.g. Google name/picture) on first join; it never
// overwrites a profile the member has since customized.
func (s *Store) SeedMemberProfile(
	ctx context.Context,
	orgDID, memberDID syntax.DID,
	displayName, avatarUrl string,
) (err error) {
	ctx, span := tracer.Start(ctx, "opensocial.SeedMemberProfile", trace.WithAttributes(
		telemetry.Org(orgDID), memberAttr(memberDID),
	))
	defer func() { telemetry.End(span, err) }()
	if displayName == "" && avatarUrl == "" {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		spacesStoreTx := s.spacesStore.WithTx(tx)
		membersSpace := habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self")
		rkey := syntax.RecordKey(memberDID)
		_, err := spacesStoreTx.GetRecord(
			ctx,
			membersSpace,
			memberDID,
			MemberProfileCollection,
			rkey,
		)
		if err == nil {
			// Already set (either self-configured or seeded by a concurrent
			// sign-in); leave it alone.
			return nil
		}
		if !errors.Is(err, spaces.ErrRecordNotFound) {
			return fmt.Errorf("get existing member profile record: %w", err)
		}
		recordBytes, err := spaces.MarshalRecord(habitat.NetworkHabitatOpensocialMemberProfile{
			DisplayName: displayName,
			AvatarUrl:   avatarUrl,
			UpdatedAt:   time.Now().Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("marshal member profile record: %w", err)
		}
		if _, _, err = spacesStoreTx.PutRecord(
			ctx, membersSpace, memberDID, MemberProfileCollection, rkey, recordBytes,
		); err != nil {
			return fmt.Errorf("put member profile record: %w", err)
		}
		return nil
	})
}

// Models returns the GORM models this package persists. Their tables are
// created by db.Migrate.
func Models() []any {
	return []any{&inviteRow{}, &joinRequestRow{}, &moderationSubjectRow{}, &moderationEventRow{}}
}
