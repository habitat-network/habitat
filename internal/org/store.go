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
	"github.com/habitat-network/habitat/internal/login"
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
	GetOrg(ctx context.Context, orgID syntax.DID) (Org, error)
	GetOrgForDID(ctx context.Context, did syntax.DID) (Org, error)
	CreateOrg(
		ctx context.Context,
		name string,
		adminHandle string,
		adminPassword string,
		loginMethod string,
		loginID string,
		handleSubdomain string,
	) (orgId *identity.Identity, id *identity.Identity, err error)

	GetMember(ctx context.Context, did syntax.DID) (*OrgMember, error)
	GetMemberByLoginID(ctx context.Context, loginID string) (*OrgMember, error)
}

// storeImpl is the Store implementation backed by gorm and the identity directory.
type storeImpl struct {
	db               *gorm.DB
	hive             hive.Hive
	dir              identity.Directory
	pearDomain       string
	everyone         Org
	passwordProvider *login.PasswordLoginProvider
}

// NewStore creates a Store that manages multiple orgs on a pear instance.
func NewStore(
	db *gorm.DB,
	hve hive.Hive,
	dir identity.Directory,
	pearDomain string,
	passwordProvider *login.PasswordLoginProvider,
) (Store, error) {
	if err := db.AutoMigrate(&organization{}, &member{}, &spentToken{}); err != nil {
		return nil, err
	}
	return &storeImpl{
		db:               db,
		hive:             hve,
		dir:              dir,
		pearDomain:       pearDomain,
		everyone:         NewEveryoneOrg(),
		passwordProvider: passwordProvider,
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
func (s *storeImpl) GetOrg(ctx context.Context, orgID syntax.DID) (Org, error) {
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
) (*identity.Identity, *identity.Identity, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, nil, err
	}
	signingSecret := base64.StdEncoding.EncodeToString(secret)

	var orgId *identity.Identity
	var id *identity.Identity
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		mintedOrgId, err := s.hive.WithTx(tx).MintOrgIdentity(ctx, handleSubdomain)
		if err != nil {
			return err
		}
		orgId = mintedOrgId
		if err := tx.Create(&organization{
			ID:              mintedOrgId.DID,
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
		// Determine the member's LoginID based on login method
		var memberLoginID string
		switch method {
		case "password":
			memberLoginID = mintedId.DID.String()

			if err := s.passwordProvider.WithTx(tx).
				AddLoginEntry(mintedId.DID, adminPassword); err != nil {
				return fmt.Errorf("failed to add login entry: %w", err)
			}
		case "atproto", "google":
			memberLoginID = loginID
		}
		return tx.Create(&member{
			OrgID:     mintedOrgId.DID,
			Did:       id.DID,
			Role:      AdminRole,
			LoginID:   memberLoginID,
			CreatedAt: time.Now(),
		}).Error
	})
	if err != nil {
		return nil, nil, err
	}

	return orgId, id, nil
}

func (s *storeImpl) GetMember(ctx context.Context, did syntax.DID) (*OrgMember, error) {
	var m member
	if err := s.db.WithContext(ctx).
		Preload("Organization").
		Where("did = ?", did).
		First(&m).
		Error; err != nil {
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

func (s *storeImpl) GetMemberByLoginID(ctx context.Context, loginID string) (*OrgMember, error) {
	var m member
	if err := s.db.WithContext(ctx).Where("login_id = ?", loginID).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMemberNotFound
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
