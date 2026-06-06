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
	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/hive"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Role string

const (
	AdminRole  Role = "admin"
	MemberRole Role = "member"
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

func isDuplicateError(err error) bool {
	return errors.Is(err, gorm.ErrDuplicatedKey)
}

// Org represents a single organization on a pear instance.
type Org interface {
	DID() syntax.DID
	// Any app-level / further authz (like teams in an org) should happen using our clique permissions model.
	// The authz in this package is only for managing identities in the
	// In the future, we may not want to be so prescriptive about the admin / member setup.

	// This is exported because other packages may want to do membership lookup
	AddAdmin(ctx context.Context, admin syntax.DID) error
	// Only support adding members through CreateNewMemberIdentity for now
	// AddMembers(ctx context.Context, members []syntax.DID) error
	GetAdmins(ctx context.Context) ([]syntax.DID, error)
	GetMembers(ctx context.Context) ([]syntax.DID, error)
	RemoveAdmin(ctx context.Context, admin syntax.DID) error
	RemoveMembers(ctx context.Context, members []syntax.DID) error
	DowngradeAdmin(ctx context.Context, admin syntax.DID) error
	IsAdmin(ctx context.Context, did syntax.DID) (bool, error)
	IsMember(ctx context.Context, did syntax.DID) (bool, error)

	loginMethod() loginMethod

	// Org member identity management; may eventually replace some of the methods above
	IssueIdentityToken(
		ctx context.Context,
		caller syntax.DID,
		reusable bool,
		expiresAt time.Time,
	) (token string, err error)
	CreateNewMemberIdentity(
		ctx context.Context,
		token string,
		internalHandle string,
		password string,
	) (*identity.Identity, error)

	db.Store[Org]
}

type inviteTokenClaims struct {
	jwt.Claims
	Reusable bool `json:"reusable"`
}

type orgImpl struct {
	orgID           syntax.DID
	hive            hive.Hive
	db              *gorm.DB
	signingSecret   []byte
	handleSubdomain string
	method          loginMethod
}

var _ Org = &orgImpl{}

func (s *orgImpl) loginMethod() loginMethod {
	return s.method
}

// DID implements [Org].
func (s *orgImpl) DID() syntax.DID {
	return s.orgID
}

func (s *orgImpl) AddAdmin(ctx context.Context, admin syntax.DID) error {
	result := s.db.WithContext(ctx).Model(&member{}).
		Where("org_id = ? AND did = ?", s.orgID, admin).
		Update("role", AdminRole)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotMember
	}
	return nil
}

func (s *orgImpl) addMemberTx(
	ctx context.Context,
	tx *gorm.DB,
	did syntax.DID,
	loginID string,
) error {
	return tx.WithContext(ctx).Create(&member{
		OrgID:   s.orgID,
		Did:     did,
		Role:    MemberRole,
		LoginID: loginID,
	}).Error
}

// BootstrapAdmin implements Store.
/*
func (s *store) bootstrapAdmin(ctx context.Context, bootstrapSecret string, admin syntax.DID) error {
	panic("unimplemented")
}
*/

func (s *orgImpl) GetAdmins(ctx context.Context) ([]syntax.DID, error) {
	var rows []member
	if err := s.db.WithContext(ctx).
		Where("org_id = ? AND role = ?", s.orgID, AdminRole).
		Find(&rows).
		Error; err != nil {
		return nil, err
	}
	dids := make([]syntax.DID, 0, len(rows))
	for _, r := range rows {
		dids = append(dids, r.Did)
	}
	return dids, nil
}

func (s *orgImpl) GetMembers(ctx context.Context) ([]syntax.DID, error) {
	var rows []member
	if err := s.db.WithContext(ctx).Where("org_id = ?", s.orgID).Find(&rows).Error; err != nil {
		return nil, err
	}
	dids := make([]syntax.DID, 0, len(rows))
	for _, r := range rows {
		dids = append(dids, r.Did)
	}
	return dids, nil
}

func (s *orgImpl) DowngradeAdmin(ctx context.Context, admin syntax.DID) error {
	var adminCount int64
	if err := s.db.WithContext(ctx).
		Model(&member{}).
		Where("org_id = ? AND role = ?", s.orgID, AdminRole).
		Count(&adminCount).
		Error; err != nil {
		return err
	}
	if adminCount < 2 {
		return ErrLastAdmin
	}
	return s.db.WithContext(ctx).
		Model(&member{}).
		Where("org_id = ? AND did = ? AND role = ?", s.orgID, admin, AdminRole).
		Update("role", MemberRole).
		Error
}

func (s *orgImpl) RemoveAdmin(ctx context.Context, admin syntax.DID) error {
	var adminCount int64
	if err := s.db.WithContext(ctx).
		Model(&member{}).
		Where("org_id = ? AND role = ?", s.orgID, AdminRole).
		Count(&adminCount).
		Error; err != nil {
		return err
	}
	if adminCount < 2 {
		return ErrLastAdmin
	}
	return s.db.WithContext(ctx).
		Where("org_id = ? AND did = ? AND role = ?", s.orgID, admin, AdminRole).
		Delete(&member{}).
		Error
}

func (s *orgImpl) RemoveMembers(ctx context.Context, members []syntax.DID) error {
	return s.db.WithContext(ctx).
		Where("org_id = ? AND did IN ? AND role = ?", s.orgID, members, MemberRole).
		Delete(&member{}).
		Error
}

func (s *orgImpl) IsAdmin(ctx context.Context, did syntax.DID) (bool, error) {
	var row member
	err := s.db.WithContext(ctx).
		Where("org_id = ? AND did = ? AND role = ?", s.orgID, did, AdminRole).
		First(&row).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (s *orgImpl) IsMember(ctx context.Context, did syntax.DID) (bool, error) {
	var row member
	err := s.db.WithContext(ctx).
		Where("org_id = ? AND did = ?", s.orgID, did).
		First(&row).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (s *orgImpl) validateIdentityToken(ctx context.Context, token string) error {
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
func (s *orgImpl) IssueIdentityToken(
	ctx context.Context,
	caller syntax.DID,
	reusable bool,
	expiresAt time.Time,
) (string, error) {
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
func (s *orgImpl) CreateNewMemberIdentity(
	ctx context.Context,
	token string,
	internalHandle string,
	password string,
) (*identity.Identity, error) {
	// Validate the token
	if err := s.validateIdentityToken(ctx, token); err != nil {
		return nil, err
	}

	passwordHash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}

	var id *identity.Identity
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// If token is valid, call into hive to mint the new identity and serve it
		newId, err := s.hive.WithTx(tx).MintIdentity(ctx, internalHandle, s.handleSubdomain)
		if err != nil {
			return fmt.Errorf("mint identity: %w", err)
		}
		id = newId
		return s.addMemberTx(ctx, tx, id.DID, passwordHash)
	})
	if err != nil {
		return nil, err
	}
	return id, nil
}

// WithTx implements [Org].
func (s *orgImpl) WithTx(tx *gorm.DB) Org {
	return &orgImpl{
		orgID:           s.orgID,
		hive:            s.hive,
		db:              tx,
		signingSecret:   s.signingSecret,
		handleSubdomain: s.handleSubdomain,
	}
}
