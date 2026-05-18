package org

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
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

	// LoginMethod returns how users authenticate: "atproto", "google", or "password".
	LoginMethod() LoginMethod

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
}

type inviteTokenClaims struct {
	jwt.Claims
	Reusable bool `json:"reusable"`
}

type orgImpl struct {
	orgID         string
	hive          hive.Hive
	db            *gorm.DB
	signingSecret []byte
}

var _ Org = &orgImpl{}

func NewOrg(
	orgID string,
	hive hive.Hive,
	db *gorm.DB,
	signingSecret []byte,
) (*orgImpl, error) {
	if err := db.AutoMigrate(&member{}, &spentToken{}); err != nil {
		return nil, err
	}
	return &orgImpl{
		orgID:         orgID,
		hive:          hive,
		db:            db,
		signingSecret: signingSecret,
	}, nil
}

func (s *orgImpl) LoginMethod() LoginMethod {
	var org organization
	if err := s.db.First(&org, "id = ?", s.orgID).Error; err != nil {
		return "password" // safe default
	}
	return org.LoginMethod
}

func (s *orgImpl) AddAdmin(ctx context.Context, admin syntax.DID) error {
	result := s.db.WithContext(ctx).Model(&member{}).
		Where("org_id = ? AND did = ?", s.orgID, admin.String()).
		Update("role", AdminRole)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotMember
	}
	return nil
}

func (s *orgImpl) addMember(ctx context.Context, did syntax.DID, loginID string) error {
	return s.addMemberTx(ctx, s.db, did, loginID)
}

func (s *orgImpl) addMemberTx(
	ctx context.Context,
	tx *gorm.DB,
	did syntax.DID,
	loginID string,
) error {
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "org_id"}, {Name: "did"}},
		DoNothing: true,
	}).Create(&member{
		OrgID:     s.orgID,
		Did:       did.String(),
		Role:      string(MemberRole),
		LoginID:   loginID,
		CreatedAt: time.Now(),
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
		did, err := syntax.ParseDID(r.Did)
		if err != nil {
			return nil, err
		}
		dids = append(dids, did)
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
		did, err := syntax.ParseDID(r.Did)
		if err != nil {
			return nil, err
		}
		dids = append(dids, did)
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
		Where("org_id = ? AND did = ? AND role = ?", s.orgID, admin.String(), AdminRole).
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
		Where("org_id = ? AND did = ? AND role = ?", s.orgID, admin.String(), AdminRole).
		Delete(&member{}).
		Error
}

func (s *orgImpl) RemoveMembers(ctx context.Context, members []syntax.DID) error {
	dids := make([]string, 0, len(members))
	for _, did := range members {
		dids = append(dids, did.String())
	}
	return s.db.WithContext(ctx).
		Where("org_id = ? AND did IN ? AND role = ?", s.orgID, dids, MemberRole).
		Delete(&member{}).
		Error
}

func (s *orgImpl) IsAdmin(ctx context.Context, did syntax.DID) (bool, error) {
	var row member
	err := s.db.WithContext(ctx).
		Where("org_id = ? AND did = ? AND role = ?", s.orgID, did.String(), AdminRole).
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
		Where("org_id = ? AND did = ?", s.orgID, did.String()).
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

func (s *orgImpl) AuthenticateMember(
	ctx context.Context,
	handle string,
	password string,
) (bool, error) {
	id, err := s.hive.LookupHandle(ctx, syntax.Handle(handle))
	if err != nil {
		return false, nil
	}

	var row member
	err = s.db.WithContext(ctx).
		Where("org_id = ? AND did = ?", s.orgID, id.DID.String()).
		First(&row).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Don't leak whether the handle exists
		return false, nil
	} else if err != nil {
		return false, err
	}

	ok, err := verifyPassword(password, row.LoginID)
	if errors.Is(err, argon2id.ErrInvalidHash) {
		// Members created before passwords were required have no usable hash.
		return false, nil
	}
	return ok, err
}
