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
	MembersSpaceType = "org.atgrp.members"
	InboxSpaceType   = "org.atgrp.inbox"
	MetaSpaceType    = "org.atgrp.meta"
)

const (
	AboutCollection         = "org.atgrp.about"
	ConfigCollection        = "org.atgrp.config"
	ConfirmationCollection  = "org.atgrp.confirmation"
	InviteCollection        = "org.atgrp.invite"
	LabelCollection         = "com.atproto.label.record"
	LabelerCollection       = "org.atgrp.labeler"
	MembershipCollection    = "org.atgrp.membership"
	RoleCollection          = "org.atgrp.role"
	RuleCollection          = "org.atgrp.rule"
	SpaceIndexCollection    = "org.atgrp.space"
	SpacePolicyCollection   = "com.atproto.space.policy"
	MemberProfileCollection = "network.habitat.opensocial.memberProfile"
)

// Join policies, as in the config record.
const (
	JoinPolicyOpen     = "open"
	JoinPolicyApproval = "approval"
	JoinPolicyInvite   = "invite"
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
		if err := putConfirmation(
			ctx, s.spacesStore.WithTx(tx), orgID.DID, creator,
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

// createOrgShell mints the org identity and writes its meta and members
// spaces, about and config records, built-in roles, and policies within tx:
// every part of org creation except its initial membership.
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
	storeTx := s.WithTx(tx)
	metaSpace, err := spacesStoreTx.CreateSpace(ctx, orgID.DID, MetaSpaceType, "self")
	if err != nil {
		return nil, fmt.Errorf("create meta space: %w", err)
	}
	membersSpace, err := spacesStoreTx.CreateSpace(ctx, orgID.DID, MembersSpaceType, "self")
	if err != nil {
		return nil, fmt.Errorf("create members space: %w", err)
	}
	for _, record := range []struct {
		space      habitat_syntax.SpaceURI
		collection syntax.NSID
		rkey       syntax.RecordKey
		value      any
	}{
		{metaSpace, AboutCollection, "self", opensocial_api.OrgAtgrpAbout{DisplayName: handle}},
		// Orgs have always been invite-only.
		{metaSpace, ConfigCollection, "self", opensocial_api.OrgAtgrpConfig{
			JoinPolicy: JoinPolicyInvite,
		}},
	} {
		recordBytes, err := spaces.MarshalRecord(record.value)
		if err != nil {
			return nil, fmt.Errorf("marshal %s record: %w", record.collection, err)
		}
		if _, _, err := spacesStoreTx.PutRecord(
			ctx, record.space, orgID.DID, record.collection, record.rkey, recordBytes,
		); err != nil {
			return nil, fmt.Errorf("put %s record: %w", record.collection, err)
		}
	}
	for rkey, role := range bootstrapRoles() {
		if err := storeTx.PutRole(ctx, orgID.DID, syntax.RecordKey(rkey), role); err != nil {
			return nil, err
		}
	}
	// The meta space is the group's public face.
	if err := storeTx.SetPolicy(ctx, metaSpace, bootstrapPolicy(true, false)); err != nil {
		return nil, err
	}
	if err := storeTx.SetPolicy(ctx, membersSpace, bootstrapPolicy(false, true)); err != nil {
		return nil, err
	}
	return orgID, nil
}

// putConfirmation writes member's org.atgrp.confirmation record, authored by
// the member, into the org's members space. The host authors it only for
// members it enrolls itself, such as the creator.
func putConfirmation(
	ctx context.Context,
	spacesStore spaces.Store,
	orgDID, member syntax.DID,
) error {
	recordBytes, err := spaces.MarshalRecord(opensocial_api.OrgAtgrpConfirmation{
		CreatedAt: time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshal confirmation record: %w", err)
	}
	if _, _, err := spacesStore.PutRecord(
		ctx, membersSpaceOf(orgDID), member, ConfirmationCollection, "self", recordBytes,
	); err != nil {
		return fmt.Errorf("put confirmation record: %w", err)
	}
	return nil
}

// putMembership writes member's org.atgrp.membership record,
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
	recordBytes, err := spaces.MarshalRecord(opensocial_api.OrgAtgrpMembership{
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

// CreateSpace creates a modality space under the group that members holding
// one of roles may read and write, and lists it in the group's space index.
// It performs no authorization check; group-host callers use CreateGroupSpace.
func (s *Store) CreateSpace(
	ctx context.Context,
	orgDID syntax.DID,
	roles []string,
	spaceType syntax.NSID,
	skey habitat_syntax.SpaceKey,
) (_ habitat_syntax.SpaceURI, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.CreateSpace", trace.WithAttributes(
		telemetry.Org(orgDID),
		rolesKey.StringSlice(roles),
		attribute.String("habitat.space.type", spaceType.String()),
	))
	defer func() { telemetry.End(span, err) }()
	policy := spaces.Policy{Type: spaces.PolicyGroupSpace}
	for _, role := range roles {
		policy.Grants = append(
			policy.Grants,
			spaces.GroupGrant{Role: role, Read: true, Write: true},
		)
	}
	return s.createSpace(ctx, orgDID, spaceType, skey, policy)
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
) (habitat_syntax.SpaceURI, error) {
	var created habitat_syntax.SpaceURI
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txStore := s.WithTx(tx)
		spaceURI, err := txStore.spacesStore.CreateSpace(ctx, orgDID, spaceType, skey)
		if err != nil {
			return fmt.Errorf("space store create: %w", err)
		}
		created = spaceURI
		return txStore.SetPolicy(ctx, spaceURI, policy)
	}); err != nil {
		return "", fmt.Errorf("create space: %w", err)
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
			ctx, metaSpace, orgDID, AboutCollection, "self",
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
			ctx, metaSpace, orgDID, AboutCollection, "self", recordBytes,
		); err != nil {
			return fmt.Errorf("put profile record: %w", err)
		}
		return nil
	}); err != nil {
		return atdata.Blob{}, fmt.Errorf("upload image: %w", err)
	}
	return blob, nil
}

// UpdateProfile rewrites the group's about record and, when joinPolicy is not
// empty, its config record.
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
		ctx, metaSpace, orgDID, AboutCollection, "self",
	)
	if err != nil {
		return fmt.Errorf("get existing about record: %w", err)
	}
	existingRecord.Value["displayName"] = name
	if description == "" {
		delete(existingRecord.Value, "description")
	} else {
		existingRecord.Value["description"] = description
	}
	recordBytes, err := spaces.MarshalRecord(existingRecord.Value)
	if err != nil {
		return fmt.Errorf("marshal about record: %w", err)
	}
	if _, _, err = s.spacesStore.PutRecord(
		ctx, metaSpace, orgDID, AboutCollection, "self", recordBytes,
	); err != nil {
		return fmt.Errorf("put about record: %w", err)
	}
	if joinPolicy == "" {
		return nil
	}
	return s.putConfig(ctx, orgDID, joinPolicy)
}

func (s *Store) putConfig(ctx context.Context, orgDID syntax.DID, joinPolicy string) error {
	recordBytes, err := spaces.MarshalRecord(opensocial_api.OrgAtgrpConfig{JoinPolicy: joinPolicy})
	if err != nil {
		return fmt.Errorf("marshal config record: %w", err)
	}
	if _, _, err := s.spacesStore.PutRecord(
		ctx, habitat_syntax.ConstructSpaceURI(orgDID, MetaSpaceType, "self"),
		orgDID, ConfigCollection, "self", recordBytes,
	); err != nil {
		return fmt.Errorf("put config record: %w", err)
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
	if err := s.keepsAdmin(ctx, orgDID, user, roles); err != nil {
		return err
	}
	recordBytes, err := spaces.MarshalRecord(opensocial_api.OrgAtgrpMembership{
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
		"org.atgrp.membership",
		syntax.RecordKey(user),
		recordBytes,
	)
	return err
}

// ListMemberSpaces returns the org.atgrp.members spaces `user`
// belongs to — every org whose members space it holds a permissioned repo
// in (i.e. has written its own record into, such as a
// org.atgrp.confirmation record when joining).
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
		"org.atgrp.membership",
		syntax.RecordKey(user),
	)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get membership record: %w", err)
	}
	var membership opensocial_api.OrgAtgrpMembership
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

// Profile is a group's about record together with its join policy, which
// lives in the config record.
type Profile struct {
	opensocial_api.OrgAtgrpAbout
	JoinPolicy string
}

// GetProfile returns the group's about record and join policy. A group with no
// config record is invite-only.
func (s *Store) GetProfile(
	ctx context.Context,
	orgDID syntax.DID,
) (_ Profile, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.GetProfile", trace.WithAttributes(
		telemetry.Org(orgDID),
	))
	defer func() { telemetry.End(span, err) }()
	metaSpace := habitat_syntax.ConstructSpaceURI(orgDID, MetaSpaceType, "self")
	profile := Profile{JoinPolicy: JoinPolicyInvite}
	aboutRecord, err := s.spacesStore.GetRecord(ctx, metaSpace, orgDID, AboutCollection, "self")
	if err != nil && !errors.Is(err, spaces.ErrRecordNotFound) {
		return Profile{}, fmt.Errorf("get about record: %w", err)
	}
	if err == nil {
		if err := decodeRecordValue(aboutRecord.Value, &profile.OrgAtgrpAbout); err != nil {
			return Profile{}, fmt.Errorf("decode about record: %w", err)
		}
	}
	configRecord, err := s.spacesStore.GetRecord(ctx, metaSpace, orgDID, ConfigCollection, "self")
	if err != nil && !errors.Is(err, spaces.ErrRecordNotFound) {
		return Profile{}, fmt.Errorf("get config record: %w", err)
	}
	if err == nil {
		var config opensocial_api.OrgAtgrpConfig
		if err := decodeRecordValue(configRecord.Value, &config); err != nil {
			return Profile{}, fmt.Errorf("decode config record: %w", err)
		}
		if config.JoinPolicy != "" {
			profile.JoinPolicy = config.JoinPolicy
		}
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
