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
	"github.com/habitat-network/habitat/internal/hive"
	"gorm.io/gorm"
)

type OrgMember struct {
	Org     Org
	DID     syntax.DID
	Role    Role
	LoginID string
}

// Store is the registry of all orgs on a pear instance.
// It routes DIDs to their org and provides cross-org membership checks.
type Store interface {
	GetOrg(ctx context.Context, orgID string) (Org, error)
	GetOrgForDID(ctx context.Context, did syntax.DID) (Org, error)
	CreateOrg(
		ctx context.Context,
		name string,
		adminHandle string,
		adminPassword string,
		loginMethod string,
		loginID string,
		handleSubdomain string,
	) (orgID string, id *identity.Identity, err error)

	GetMember(ctx context.Context, did syntax.DID) (*OrgMember, error)
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
func NewStore(
	db *gorm.DB,
	hve hive.Hive,
	dir identity.Directory,
	pearDomain string,
) (Store, error) {
	if err := db.AutoMigrate(&organization{}, &member{}, &spentToken{}); err != nil {
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

func (s *storeImpl) orgFromModel(org *organization) (*orgImpl, error) {
	signingSecret, err := base64.StdEncoding.DecodeString(org.SigningSecret)
	if err != nil {
		return nil, err
	}
	return &orgImpl{
		orgID:           org.ID,
		hive:            s.hive,
		db:              s.db,
		signingSecret:   signingSecret,
		handleSubdomain: org.HandleSubdomain,
		method:          org.LoginMethod,
	}, nil
}

// GetOrg returns the org with the given ID.
func (s *storeImpl) GetOrg(ctx context.Context, orgID string) (Org, error) {
	var org organization
	if err := s.db.WithContext(ctx).Where("id = ?", orgID).First(&org).Error; err != nil {
		return nil, ErrOrgNotFound
	}
	return s.orgFromModel(&org)
}

// GetOrgForDID returns the org the given DID belongs to.
// First checks the member table. If the DID isn't in any org, checks if
// it's managed by our hive. Otherwise returns the everyone org for external DIDs.
func (s *storeImpl) GetOrgForDID(ctx context.Context, did syntax.DID) (Org, error) {
	var m member
	if err := s.db.WithContext(ctx).Where("did = ?", did).First(&m).Error; err == nil {
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

// CreateOrg creates a new org with a bootstrap admin member and returns the generated org ID and the admin identity.
func (s *storeImpl) CreateOrg(
	ctx context.Context,
	name string,
	adminHandle string,
	adminPassword string,
	method string,
	loginID string,
	handleSubdomain string,
) (string, *identity.Identity, error) {
	orgBytes := make([]byte, 16)
	if _, err := rand.Read(orgBytes); err != nil {
		return "", nil, err
	}
	orgID := fmt.Sprintf("%x", orgBytes)

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", nil, err
	}
	signingSecret := base64.StdEncoding.EncodeToString(secret)

	// Determine the member's LoginID based on login method
	var memberLoginID string
	switch method {
	case "password":
		hash, err := hashPassword(adminPassword)
		if err != nil {
			return "", nil, err
		}
		memberLoginID = hash
	case "atproto", "google":
		memberLoginID = loginID
	}

	var id *identity.Identity
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&organization{
			ID:              orgID,
			Name:            name,
			LoginMethod:     loginMethod(method),
			SigningSecret:   signingSecret,
			CreatedAt:       time.Now(),
			HandleSubdomain: handleSubdomain,
		}).Error; err != nil {
			if isDuplicateError(err) {
				return ErrOrgAlreadyExists
			}
			return err
		}
		// Mint identity for the admin
		mintedId, err := s.hive.WithTx(tx).MintIdentity(ctx, adminHandle, handleSubdomain)
		if err != nil {
			return err
		}
		id = mintedId
		return tx.Create(&member{
			OrgID:     orgID,
			Did:       id.DID,
			Role:      AdminRole,
			LoginID:   memberLoginID,
			CreatedAt: time.Now(),
		}).Error
	})
	if err != nil {
		return "", nil, err
	}

	return orgID, id, nil
}

func (s *storeImpl) GetMember(ctx context.Context, did syntax.DID) (*OrgMember, error) {
	var m member
	if err := s.db.WithContext(ctx).Where("did = ?", did).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &OrgMember{
				Org:     s.everyone,
				DID:     did,
				Role:    MemberRole,
				LoginID: did.String(),
			}, nil
		}
		return nil, fmt.Errorf("failed to get member: %w", err)
	}
	org, err := s.orgFromModel(&m.Organization)
	if err != nil {
		return nil, fmt.Errorf("failed to get org from model: %w", err)
	}
	return &OrgMember{
		Org:     org,
		DID:     m.Did,
		Role:    m.Role,
		LoginID: m.LoginID,
	}, nil
}
