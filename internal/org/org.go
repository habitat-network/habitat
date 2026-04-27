package org

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
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
	ErrInvalidToken       = errors.New("an invalid token was presented")
	ErrInvalidTokenExpiry = errors.New("token expiry must be < 1 month from now")
)

// Store is used to manage the organization tied to a pear node
// It enforces permissions on its methods; i.e non-admins cannot add admins or members.
//
// Eventually we can add management scopes to member vs. admin roles, to allow members to add other members etc.
type Org interface {
	// Any app-level / further authz (like teams in an org) should happen using our clique permissions model.
	// The authz in this package is only for managing identities in the
	// In the future, we may not want to be so prescriptive about the admin / member setup.

	// This is exported because other packages may want to do membership lookup
	AddAdmin(ctx context.Context, admin syntax.DID) error
	AddMembers(ctx context.Context, members []syntax.DID) error
	GetAdmins(ctx context.Context) ([]syntax.DID, error)
	GetMembers(ctx context.Context) ([]syntax.DID, error)
	RemoveAdmin(ctx context.Context, admin syntax.DID) error
	RemoveMembers(ctx context.Context, members []syntax.DID) error
	DowngradeAdmin(ctx context.Context, admin syntax.DID) error
	IsAdmin(ctx context.Context, did syntax.DID) (bool, error)
	IsMember(ctx context.Context, did syntax.DID) (bool, error)

	// Org member identity management; may eventually replace some of the methods above
	IssueIdentityToken(ctx context.Context, caller syntax.DID, reusable bool, expiresAt time.Time) (token string, err error)
	CreateNewMemberIdentity(ctx context.Context, token string, internalHandle string) (*identity.Identity, error)

	// Generic config about this org
	GetMetadata() habitat.NetworkHabitatOrgGetMetadataOutput
}

// Keep track of members in the
type member struct {
	Member string `gorm:"primaryKey"`
	Role   string

	// Automatically populated by gorm
	CreatedAt time.Time
}

// spentToken tracks consumed single-use invite tokens by their JWT ID.
type spentToken struct {
	JTI        string    `gorm:"primaryKey"`
	ConsumedAt time.Time `gorm:"not null"`
}

type inviteTokenClaims struct {
	jwt.Claims
	Reusable bool `json:"reusable"`
}

type store struct {
	// The domain where this org is hosted; for config
	// Eventually turn this into more enriched metadata about this rog
	domain string

	// Identity management service for orgs
	hive hive.Hive

	// Manages all backing data for an org
	// Currently just an org_members table
	db *gorm.DB

	// HMAC-SHA256 key for signing and verifying invite tokens
	signingSecret []byte
}

var _ Org = &store{}

func NewOrg(domain string, hive hive.Hive, db *gorm.DB, signingSecret []byte) (Org, error) {
	if err := db.AutoMigrate(&member{}, &spentToken{}); err != nil {
		return nil, err
	}
	return &store{
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
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "member"}},
		DoUpdates: clause.Assignments(map[string]any{"role": Admin}),
	}).Create(&member{
		Member:    admin.String(),
		Role:      string(Admin),
		CreatedAt: time.Now(),
	}).Error
}

func (s *store) AddMembers(ctx context.Context, members []syntax.DID) error {
	rows := make([]member, 0, len(members))
	for _, did := range members {
		rows = append(rows, member{
			Member:    did.String(),
			Role:      string(Member),
			CreatedAt: time.Now(),
		})
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "member"}},
		DoNothing: true,
	}).Create(&rows).Error
}

// BootstrapAdmin implements Store.
/*
func (s *store) bootstrapAdmin(ctx context.Context, bootstrapSecret string, admin syntax.DID) error {
	panic("unimplemented")
}
*/

func (s *store) GetAdmins(ctx context.Context) ([]syntax.DID, error) {
	var rows []member
	if err := s.db.WithContext(ctx).Where("role = ?", Admin).Find(&rows).Error; err != nil {
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
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
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
	if err := s.db.WithContext(ctx).Model(&member{}).Where("role = ?", Admin).Count(&adminCount).Error; err != nil {
		return err
	}
	if adminCount < 2 {
		return ErrLastAdmin
	}
	return s.db.WithContext(ctx).Model(&member{}).Where("member = ? AND role = ?", admin.String(), Admin).Update("role", Member).Error
}

func (s *store) RemoveAdmin(ctx context.Context, admin syntax.DID) error {
	var adminCount int64
	if err := s.db.WithContext(ctx).Model(&member{}).Where("role = ?", Admin).Count(&adminCount).Error; err != nil {
		return err
	}
	if adminCount < 2 {
		return ErrLastAdmin
	}
	return s.db.WithContext(ctx).Where("member = ? AND role = ?", admin.String(), Admin).Delete(&member{}).Error
}

func (s *store) RemoveMembers(ctx context.Context, members []syntax.DID) error {
	dids := make([]string, 0, len(members))
	for _, did := range members {
		dids = append(dids, did.String())
	}
	return s.db.WithContext(ctx).Where("member IN ? AND role = ?", dids, Member).Delete(&member{}).Error
}

func (s *store) IsAdmin(ctx context.Context, did syntax.DID) (bool, error) {
	var row member
	err := s.db.WithContext(ctx).Where("member = ? AND role = ?", did.String(), Admin).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

// isMember implements Store.
func (s *store) IsMember(ctx context.Context, did syntax.DID) (bool, error) {
	var row member
	err := s.db.WithContext(ctx).Where("member = ?", did.String()).First(&row).Error
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
			Create(&spentToken{JTI: claims.ID, ConsumedAt: time.Now()})
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
func (s *store) CreateNewMemberIdentity(ctx context.Context, token string, internalHandle string) (*identity.Identity, error) {
	// Validate the token
	err := s.validateIdentityToken(ctx, token)
	if err != nil {
		return nil, err
	}

	// If token is valid, call into hive to mint the new identity and serve it
	id, err := s.hive.MintIdentity(internalHandle)
	if err != nil {
		return nil, err
	}

	// TODO: we could have orphaned identities if the next step fails; should handle this somehow.

	// Automatically add this member to the org
	err = s.AddMembers(ctx, []syntax.DID{id.DID})
	if err != nil {
		return nil, err
	}

	return id, nil
}
