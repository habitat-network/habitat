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

type OrgManager struct {
	db           *gorm.DB
	publicDomain string
	secret       string
}

func NewOrgManager(db *gorm.DB, publicDomain, secret string) *OrgManager {
	return &OrgManager{db: db, publicDomain: publicDomain, secret: secret}
}

func (m *OrgManager) CreateOrg(ctx context.Context, did syntax.DID, sessionID string) error {
	return m.db.WithContext(ctx).Save(&managedOrg{DID: did, SessionID: sessionID}).Error
}

func (m *OrgManager) GetOrg(ctx context.Context, did syntax.DID) (*OrgInfo, error) {
	var org managedOrg
	if err := m.db.WithContext(ctx).Where("did = ?", did).First(&org).Error; err != nil {
		return nil, err
	}
	if org.SessionID == "" {
		return nil, fmt.Errorf("no session for org %s", did)
	}
	return &OrgInfo{DID: org.DID.String()}, nil
}

func (m *OrgManager) ListOrgs(ctx context.Context) ([]OrgInfo, error) {
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
