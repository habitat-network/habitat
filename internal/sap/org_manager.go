package sap

import (
	"context"
	"errors"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
)

var ErrOrgNotFound = errors.New("org not found")

type orgManager struct {
	db *gorm.DB
}

func newOrgManager(db *gorm.DB) *orgManager {
	return &orgManager{db: db}
}

func (m *orgManager) AddManagedOrg(
	ctx context.Context,
	did syntax.DID,
	sessionID string,
) (*managedOrg, error) {
	org := &managedOrg{DID: did, SessionID: sessionID}
	if err := m.db.WithContext(ctx).
		Save(&managedOrg{DID: did, SessionID: sessionID}).
		Error; err != nil {
		return nil, err
	}
	return org, nil
}

func (m *orgManager) GetManagedOrg(ctx context.Context, did syntax.DID) (*managedOrg, error) {
	var org managedOrg
	if err := m.db.WithContext(ctx).Where("did = ?", did).First(&org).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrOrgNotFound
		}
		return nil, err
	}
	return &org, nil
}

func (m *orgManager) ListManagedOrgs(ctx context.Context) ([]syntax.DID, error) {
	var orgs []managedOrg
	if err := m.db.WithContext(ctx).Find(&orgs).Error; err != nil {
		return nil, err
	}
	var out []syntax.DID
	for _, o := range orgs {
		out = append(out, o.DID)
	}
	return out, nil
}
