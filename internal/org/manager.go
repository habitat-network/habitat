package org

import (
	"context"
	"errors"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/pkg/org"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrNotAdmin  = errors.New("caller is not an admin")
	ErrLastAdmin = errors.New("org must have at least one admin")
)

// org.Manager is used to manage the organization tied to a pear node
// It enforces permissions on its methods; i.e non-admins cannot add admins or members.
//
// Eventually we can add management scopes to member vs. admin roles, to allow members to add other members etc.
type Manager interface {
	// Function for one time use to bootstrap the admin who created the org after provisioning necessary infra.
	BootstrapAdmin(ctx context.Context, bootstrapSecret string, admin syntax.DID) error

	// It is expected that all methods will only be called by members of the org (authn has happened, non-org members rejected).
	// The Manager is responsible for doing authz within these methods, if the method action cannot be taken by anyone in the org.

	// Get* is call-able by anyone in the org.
	GetAdmins(ctx context.Context) ([]syntax.DID, error)
	GetMembers(ctx context.Context) ([]syntax.DID, error)

	// Only admins can call the following commands.
	AddAdmin(ctx context.Context, actor syntax.DID, admin syntax.DID) error
	AddMembers(ctx context.Context, actor syntax.DID, members []syntax.DID) error
	RemoveAdmin(ctx context.Context, actor syntax.DID, admin syntax.DID) error
	RemoveMembers(ctx context.Context, actor syntax.DID, members []syntax.DID) error

	// Any app-level / further authz (like teams in an org) should happen using our clique permissions model.
	// The authz in this package is only for managing identities in the org.
	// In the future, we may not want to be so prescriptive about the admin / member setup.

	// For internal use only by the middleware
	isMember(ctx context.Context, member syntax.DID) (bool, error)
}

// Keep track of members in the org.
type member struct {
	Member    string `gorm:"primaryKey"`
	Role      string
	CreatedAt time.Time // when this member was added
}

type manager struct {
	org org.Org

	// Manages all backing data for an org
	// Currently just an org_members table
	db *gorm.DB
}

func NewManager(org org.Org, db *gorm.DB) (Manager, error) {
	if err := db.AutoMigrate(&member{}); err != nil {
		return nil, err
	}
	return &manager{
		org: org,
		db:  db,
	}, nil
}

// AddAdmin implements Manager.
func (m *manager) AddAdmin(ctx context.Context, actor syntax.DID, admin syntax.DID) error {
	isAdmin, err := m.isAdmin(ctx, actor)
	if err != nil {
		return err
	}
	if !isAdmin {
		return ErrNotAdmin
	}
	return m.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "member"}},
		DoUpdates: clause.Assignments(map[string]any{"role": org.Admin}),
	}).Create(&member{
		Member:    admin.String(),
		Role:      string(org.Admin),
		CreatedAt: time.Now(),
	}).Error
}

// AddMembers implements Manager.
func (m *manager) AddMembers(ctx context.Context, actor syntax.DID, members []syntax.DID) error {
	isAdmin, err := m.isAdmin(ctx, actor)
	if err != nil {
		return err
	}
	if !isAdmin {
		return ErrNotAdmin
	}
	rows := make([]member, 0, len(members))
	for _, did := range members {
		rows = append(rows, member{
			Member:    did.String(),
			Role:      string(org.Member),
			CreatedAt: time.Now(),
		})
	}
	return m.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "member"}},
		DoNothing: true,
	}).Create(&rows).Error
}

// BootstrapAdmin implements Manager.
func (m *manager) BootstrapAdmin(ctx context.Context, bootstrapSecret string, admin syntax.DID) error {
	panic("unimplemented")
}

// GetAdmins implements Manager.
func (m *manager) GetAdmins(ctx context.Context) ([]syntax.DID, error) {
	var rows []member
	if err := m.db.WithContext(ctx).Where("role = ?", org.Admin).Find(&rows).Error; err != nil {
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

// GetMembers implements Manager.
func (m *manager) GetMembers(ctx context.Context) ([]syntax.DID, error) {
	var rows []member
	if err := m.db.WithContext(ctx).Find(&rows).Error; err != nil {
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

// RemoveAdmin implements Manager.
func (m *manager) RemoveAdmin(ctx context.Context, actor syntax.DID, admin syntax.DID) error {
	isAdmin, err := m.isAdmin(ctx, actor)
	if err != nil {
		return err
	}
	if !isAdmin {
		return ErrNotAdmin
	}
	var adminCount int64
	if err := m.db.WithContext(ctx).Model(&member{}).Where("role = ?", org.Admin).Count(&adminCount).Error; err != nil {
		return err
	}
	if adminCount < 2 {
		return ErrLastAdmin
	}
	return m.db.WithContext(ctx).Where("member = ? AND role = ?", admin.String(), org.Admin).Delete(&member{}).Error
}

// RemoveMembers implements Manager.
func (m *manager) RemoveMembers(ctx context.Context, actor syntax.DID, members []syntax.DID) error {
	isAdmin, err := m.isAdmin(ctx, actor)
	if err != nil {
		return err
	}
	if !isAdmin {
		return ErrNotAdmin
	}
	dids := make([]string, 0, len(members))
	for _, did := range members {
		dids = append(dids, did.String())
	}
	return m.db.WithContext(ctx).Where("member IN ? AND role = ?", dids, org.Member).Delete(&member{}).Error
}

func (m *manager) isAdmin(ctx context.Context, did syntax.DID) (bool, error) {
	var row member
	err := m.db.WithContext(ctx).Where("member = ? AND role = ?", did.String(), org.Admin).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

// isMember implements Manager.
func (m *manager) isMember(ctx context.Context, did syntax.DID) (bool, error) {
	var row member
	err := m.db.WithContext(ctx).Where("member = ?", did.String()).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

var _ Manager = &manager{}
