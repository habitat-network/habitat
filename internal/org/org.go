package org

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
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

// TODO: we should clean up this table
type identityToken struct {
	TokenHash string `gorm:"primaryKey"`
	Reusable  bool
	ExpiresAt time.Time  `gorm:"not null"`
	IssuedBy  string     // DID of admin who created this token
	UsedAt    *time.Time // nil until consumed (only meaningful for non-reusable)

	// Automatically populated by gorm
	CreatedAt time.Time
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
}

var _ Org = &store{}

func NewOrg(domain string, hive hive.Hive, db *gorm.DB) (Org, error) {
	if err := db.AutoMigrate(&member{}, &identityToken{}); err != nil {
		return nil, err
	}
	return &store{
		domain: domain,
		hive:   hive,
		db:     db,
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

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Returns ErrTokenDenied if the token could not be validated, otherwise an error if unable to validate, otherwise nil
func (s *store) validateIdentityToken(ctx context.Context, token string) error {
	hash := hashToken(token)

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var t identityToken
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}). // lock on this row to prevent TOCTOU race
									Where("token_hash = ?", hash).
									First(&t).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidToken
		}
		if err != nil {
			return err
		}

		if time.Now().After(t.ExpiresAt) {
			return ErrInvalidToken
		}

		if !t.Reusable {
			if t.UsedAt != nil {
				return ErrInvalidToken
			}
			now := time.Now()
			if err := tx.Model(&t).Update("used_at", &now).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// IssueIdentityToken implements Org.
func (s *store) IssueIdentityToken(ctx context.Context, caller syntax.DID, reusable bool, expiresAt time.Time) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b)

	if expiresAt.After(time.Now().AddDate(0, 1, 0)) {
		return "", ErrInvalidTokenExpiry
	}

	row := identityToken{
		TokenHash: hashToken(token),
		Reusable:  reusable,
		ExpiresAt: expiresAt,
		IssuedBy:  caller.String(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return "", err
	}
	return token, nil
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
