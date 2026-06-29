package sap

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
)

// OrgInfo is the public view of a managed org.
type OrgInfo struct {
	DID string `json:"did"`
}

type orgManager struct {
	db           *gorm.DB
	publicDomain string
	secret       string
}

func newOrgManager(db *gorm.DB, publicDomain, secret string) *orgManager {
	return &orgManager{db: db, publicDomain: publicDomain, secret: secret}
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

func (m *orgManager) GetManagedOrg(ctx context.Context, did syntax.DID) (*OrgInfo, error) {
	var org managedOrg
	if err := m.db.WithContext(ctx).Where("did = ?", did).First(&org).Error; err != nil {
		return nil, err
	}
	if org.SessionID == "" {
		return nil, fmt.Errorf("no session for org %s", did)
	}
	return &OrgInfo{DID: org.DID.String()}, nil
}

func (m *orgManager) ListManagedOrgs(ctx context.Context) ([]OrgInfo, error) {
	var orgs []managedOrg
	if err := m.db.WithContext(ctx).Find(&orgs).Error; err != nil {
		return nil, err
	}
	var out []OrgInfo
	for _, o := range orgs {
		if o.SessionID != "" {
			out = append(out, OrgInfo{DID: o.DID.String()})
		}
	}
	return out, nil
}
