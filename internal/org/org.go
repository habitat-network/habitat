package org

import (
	"context"
	"errors"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/core"
	"gorm.io/gorm"
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

type orgImpl struct {
	orgID           syntax.DID
	name            string
	db              *gorm.DB
	handleSubdomain string
	method          core.LoginMethod
}

var _ core.Org = &orgImpl{}

// DID implements [Org].
func (s *orgImpl) DID() syntax.DID {
	return s.orgID
}

func (s *orgImpl) GetMetadata(
	ctx context.Context,
	domain string,
) habitat.NetworkHabitatOrgGetMetadataOutput {
	return habitat.NetworkHabitatOrgGetMetadataOutput{
		LoginMethod:     string(s.method),
		HandleSubdomain: s.handleSubdomain,
		OrgId:           string(s.orgID),
		Name:            s.name,
	}
}

func (s *orgImpl) LoginMethod(ctx context.Context) core.LoginMethod {
	return s.method
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

// WithTx implements [Org].
func (s *orgImpl) WithTx(tx *gorm.DB) core.Org {
	return &orgImpl{
		orgID:           s.orgID,
		db:              tx,
		handleSubdomain: s.handleSubdomain,
	}
}
