package org

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/hive"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Role string

const (
	Admin  Role = "admin"
	Member Role = "member"
)

var (
	ErrNotAdmin           = errors.New("caller is not an admin")
	ErrLastAdmin          = errors.New("org must have at least one admin")
	ErrNotMember          = errors.New("DID is not a member of this org")
	ErrInvalidToken       = errors.New("an invalid token was presented")
	ErrInvalidTokenExpiry = errors.New("token expiry must be < 1 month from now")
	ErrOrgNotFound        = errors.New("organization not found")
	ErrMemberNotFound     = errors.New("member not found in any org")
	ErrOrgAlreadyExists   = errors.New("organization already exists")
)

// Org represents a single organization on a pear instance.
type Org interface {
	AddAdmin(ctx context.Context, admin syntax.DID) error
	GetAdmins(ctx context.Context) ([]syntax.DID, error)
	GetMembers(ctx context.Context) ([]syntax.DID, error)
	RemoveAdmin(ctx context.Context, admin syntax.DID) error
	RemoveMembers(ctx context.Context, members []syntax.DID) error
	DowngradeAdmin(ctx context.Context, admin syntax.DID) error
	IsAdmin(ctx context.Context, did syntax.DID) (bool, error)
	IsMember(ctx context.Context, did syntax.DID) (bool, error)

	IssueIdentityToken(ctx context.Context, caller syntax.DID, reusable bool, expiresAt time.Time) (token string, err error)
	CreateNewMemberIdentity(ctx context.Context, token string, internalHandle string, password string) (*identity.Identity, error)
	AuthenticateMember(ctx context.Context, handle string, password string) (bool, error)

	GetMetadata() habitat.NetworkHabitatOrgGetMetadataOutput
}

// Store is the registry of all orgs on a pear instance.
// It routes DIDs to their org and provides cross-org membership checks.
type Store interface {
	GetOrg(ctx context.Context, orgID string) (Org, error)
	GetOrgByDID(ctx context.Context, did syntax.DID) (Org, error)
	AuthenticateMember(ctx context.Context, handle string, password string) (bool, error)
	CreateOrg(ctx context.Context, domain string, adminHandle string, adminPassword string) (orgID string, id *identity.Identity, err error)
}

type inviteTokenClaims struct {
	jwt.Claims
	Reusable bool `json:"reusable"`
}

type store struct {
	orgID         string
	domain        string
	hive          hive.Hive
	db            *gorm.DB
	signingSecret []byte
}

var _ Org = &store{}

func NewOrg(orgID string, domain string, hive hive.Hive, db *gorm.DB, signingSecret []byte) (*store, error) {
	if err := db.AutoMigrate(&member{}, &spentToken{}); err != nil {
		return nil, err
	}
	return &store{
		orgID:         orgID,
		domain:        domain,
		hive:          hive,
		db:            db,
		signingSecret: signingSecret,
	}, nil
}

// GetConfig implements Org.
func (s *store) GetMetadata() habitat.NetworkHabitatOrgGetMetadataOutput {
	return habitat.NetworkHabitatOrgGetMetadataOutput{
		Domain: s.domain,
	}
}

func (s *store) AddAdmin(ctx context.Context, admin syntax.DID) error {
	result := s.db.WithContext(ctx).Model(&member{}).
		Where("org_id = ? AND member = ?", s.orgID, admin.String()).
		Update("role", Admin)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotMember
	}
	return nil
}

func (s *store) addMember(ctx context.Context, did syntax.DID, passwordHash string) error {
	return s.addMemberTx(ctx, s.db, did, passwordHash)
}

func (s *store) addMemberTx(ctx context.Context, tx *gorm.DB, did syntax.DID, passwordHash string) error {
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "org_id"}, {Name: "member"}},
		DoNothing: true,
	}).Create(&member{
		OrgID:        s.orgID,
		Member:       did.String(),
		Role:         string(Member),
		PasswordHash: passwordHash,
		CreatedAt:    time.Now(),
	}).Error
}

// BootstrapAdmin implements Store.
/*
func (s *store) bootstrapAdmin(ctx context.Context, bootstrapSecret string, admin syntax.DID) error {
	panic("unimplemented")
}
*/

func (s *store) GetAdmins(ctx context.Context) ([]syntax.DID, error) {
	var rows []member
	if err := s.db.WithContext(ctx).Where("org_id = ? AND role = ?", s.orgID, Admin).Find(&rows).Error; err != nil {
		return nil, err
	}
	dids := make([]syntax.DID, 0, len(rows))
	for _, r := range rows {
		did, err := syntax.ParseDID(r.Member)
		if err != nil {
			return nil, err
		}
		dids = append(dids, did)
	}
	return dids, nil
}

func (s *store) GetMembers(ctx context.Context) ([]syntax.DID, error) {
	var rows []member
	if err := s.db.WithContext(ctx).Where("org_id = ?", s.orgID).Find(&rows).Error; err != nil {
		return nil, err
	}
	dids := make([]syntax.DID, 0, len(rows))
	for _, r := range rows {
		did, err := syntax.ParseDID(r.Member)
		if err != nil {
			return nil, err
		}
		dids = append(dids, did)
	}
	return dids, nil
}

func (s *store) DowngradeAdmin(ctx context.Context, admin syntax.DID) error {
	var adminCount int64
	if err := s.db.WithContext(ctx).Model(&member{}).Where("org_id = ? AND role = ?", s.orgID, Admin).Count(&adminCount).Error; err != nil {
		return err
	}
	if adminCount < 2 {
		return ErrLastAdmin
	}
	return s.db.WithContext(ctx).Model(&member{}).Where("org_id = ? AND member = ? AND role = ?", s.orgID, admin.String(), Admin).Update("role", Member).Error
}

func (s *store) RemoveAdmin(ctx context.Context, admin syntax.DID) error {
	var adminCount int64
	if err := s.db.WithContext(ctx).Model(&member{}).Where("org_id = ? AND role = ?", s.orgID, Admin).Count(&adminCount).Error; err != nil {
		return err
	}
	if adminCount < 2 {
		return ErrLastAdmin
	}
	return s.db.WithContext(ctx).Where("org_id = ? AND member = ? AND role = ?", s.orgID, admin.String(), Admin).Delete(&member{}).Error
}

func (s *store) RemoveMembers(ctx context.Context, members []syntax.DID) error {
	dids := make([]string, 0, len(members))
	for _, did := range members {
		dids = append(dids, did.String())
	}
	return s.db.WithContext(ctx).Where("org_id = ? AND member IN ? AND role = ?", s.orgID, dids, Member).Delete(&member{}).Error
}

func (s *store) IsAdmin(ctx context.Context, did syntax.DID) (bool, error) {
	var row member
	err := s.db.WithContext(ctx).Where("org_id = ? AND member = ? AND role = ?", s.orgID, did.String(), Admin).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (s *store) IsMember(ctx context.Context, did syntax.DID) (bool, error) {
	var row member
	err := s.db.WithContext(ctx).Where("org_id = ? AND member = ?", s.orgID, did.String()).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (s *store) validateIdentityToken(ctx context.Context, token string) error {
	parsed, err := jwt.ParseSigned(token)
	if err != nil {
		return ErrInvalidToken
	}

	var claims inviteTokenClaims
	if err := parsed.Claims(s.signingSecret, &claims); err != nil {
		return ErrInvalidToken
	}

	if err := claims.ValidateWithLeeway(jwt.Expected{Time: time.Now()}, 0); err != nil {
		return ErrInvalidToken
	}

	if !claims.Reusable {
		result := s.db.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&spentToken{OrgID: s.orgID, JTI: claims.ID, ConsumedAt: time.Now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrInvalidToken
		}
	}
	return nil
}

// IssueIdentityToken implements Org.
func (s *store) IssueIdentityToken(ctx context.Context, caller syntax.DID, reusable bool, expiresAt time.Time) (string, error) {
	if expiresAt.After(time.Now().AddDate(0, 1, 0)) {
		return "", ErrInvalidTokenExpiry
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	jti := base64.RawURLEncoding.EncodeToString(b)

	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: s.signingSecret}, nil)
	if err != nil {
		return "", err
	}

	claims := inviteTokenClaims{
		Claims: jwt.Claims{
			ID:     jti,
			Issuer: caller.String(),
			Expiry: jwt.NewNumericDate(expiresAt),
		},
		Reusable: reusable,
	}
	return jwt.Signed(sig).Claims(claims).CompactSerialize()
}

// CreateNewMemberIdentity implements Org.
func (s *store) CreateNewMemberIdentity(ctx context.Context, token string, internalHandle string, password string) (*identity.Identity, error) {
	// Validate the token
	if err := s.validateIdentityToken(ctx, token); err != nil {
		return nil, err
	}

	passwordHash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}

	// If token is valid, call into hive to mint the new identity and serve it
	id, persistIdent, err := s.hive.MintIdentity(internalHandle)
	if err != nil {
		return nil, err
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := persistIdent(tx); err != nil {
			return err
		}
		return s.addMemberTx(ctx, tx, id.DID, passwordHash)
	})
	if err != nil {
		return nil, err
	}

	return id, nil
}

func (s *store) AuthenticateMember(ctx context.Context, handle string, password string) (bool, error) {
	id, err := s.hive.LookupHandle(ctx, syntax.Handle(handle))
	if err != nil {
		return false, nil
	}

	var row member
	err = s.db.WithContext(ctx).Where("org_id = ? AND member = ?", s.orgID, id.DID.String()).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}

	return verifyPassword(password, row.PasswordHash)
}

// storeImpl is the Store implementation backed by gorm and the identity directory.
type storeImpl struct {
	db         *gorm.DB
	hive       hive.Hive
	dir        identity.Directory
	pearDomain string
	everyone   Org
}

var _ Store = &storeImpl{}

// NewStore creates a Store that manages multiple orgs on a pear instance.
func NewStore(db *gorm.DB, hve hive.Hive, dir identity.Directory, pearDomain string) (Store, error) {
	if err := db.AutoMigrate(&Organization{}, &member{}, &spentToken{}); err != nil {
		return nil, err
	}
	return &storeImpl{
		db:         db,
		hive:       hve,
		dir:        dir,
		pearDomain: pearDomain,
		everyone:   NewEveryoneOrg(),
	}, nil
}

func (s *storeImpl) orgFromModel(org *Organization) (*store, error) {
	signingSecret, err := base64.StdEncoding.DecodeString(org.SigningSecret)
	if err != nil {
		return nil, err
	}
	return &store{
		orgID:         org.ID,
		domain:        org.Domain,
		hive:          s.hive,
		db:            s.db,
		signingSecret: signingSecret,
	}, nil
}

// GetOrg returns the org with the given ID.
func (s *storeImpl) GetOrg(ctx context.Context, orgID string) (Org, error) {
	var org Organization
	if err := s.db.WithContext(ctx).Where("id = ?", orgID).First(&org).Error; err != nil {
		return nil, ErrOrgNotFound
	}
	return s.orgFromModel(&org)
}

// GetOrgByDID returns the org the given DID belongs to.
// First checks the member table. If the DID isn't in any org, checks if
// it's managed by our hive. Otherwise returns the everyone org for external DIDs.
func (s *storeImpl) GetOrgByDID(ctx context.Context, did syntax.DID) (Org, error) {
	var m member
	if err := s.db.WithContext(ctx).Where("member = ?", did.String()).First(&m).Error; err == nil {
		return s.GetOrg(ctx, m.OrgID)
	}

	id, err := s.dir.LookupDID(ctx, did)
	if err != nil {
		return s.everyone, nil
	}

	svc, hasHabitat := id.Services["habitat"]
	if hasHabitat && svc.URL == "https://"+s.pearDomain {
		return nil, ErrMemberNotFound
	}

	return s.everyone, nil
}

// AuthenticateMember authenticates a member by handle across all orgs.
func (s *storeImpl) AuthenticateMember(ctx context.Context, handle string, password string) (bool, error) {
	id, err := s.hive.LookupHandle(ctx, syntax.Handle(handle))
	if err != nil {
		return false, nil
	}

	org, err := s.GetOrgByDID(ctx, id.DID)
	if err != nil {
		return false, nil
	}

	return org.AuthenticateMember(ctx, handle, password)
}

// CreateOrg creates a new org with a bootstrap admin member and returns the generated org ID and the admin identity.
func (s *storeImpl) CreateOrg(ctx context.Context, domain string, adminHandle string, adminPassword string) (string, *identity.Identity, error) {
	// Generate a random org ID
	orgBytes := make([]byte, 16)
	if _, err := rand.Read(orgBytes); err != nil {
		return "", nil, err
	}
	orgID := fmt.Sprintf("%x", orgBytes)

	// Generate signing secret
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", nil, err
	}
	signingSecret := base64.StdEncoding.EncodeToString(secret)

	// Hash the admin password
	passwordHash, err := hashPassword(adminPassword)
	if err != nil {
		return "", nil, err
	}

	// Mint identity for the admin
	id, persistIdent, err := s.hive.MintIdentity(adminHandle)
	if err != nil {
		return "", nil, err
	}

	// Persist org, identity, and admin member in a single transaction
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&Organization{
			ID:            orgID,
			Domain:        domain,
			SigningSecret: signingSecret,
			CreatedAt:     time.Now(),
		}).Error; err != nil {
			return err
		}
		if err := persistIdent(tx); err != nil {
			return err
		}
		return tx.Create(&member{
			OrgID:        orgID,
			Member:       id.DID.String(),
			Role:         string(Admin),
			PasswordHash: passwordHash,
			CreatedAt:    time.Now(),
		}).Error
	})
	if err != nil {
		return "", nil, err
	}

	return orgID, id, nil
}
