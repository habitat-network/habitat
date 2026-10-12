package emaildomain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
)

var (
	ErrDomainTaken      = errors.New("email domain is already mapped to an org")
	ErrEmailProvisioned = errors.New("email is already provisioned")
	ErrWorkOSOrgTaken   = errors.New("workos organization is already mapped to an org")
)

// domainMapping routes an email domain to the org that owns it and the
// login method members of that domain use.
type domainMapping struct {
	Domain      string      `gorm:"primaryKey"` // e.g. "acme.com"
	OrgDID      syntax.DID  `gorm:"column:org_did;not null"`
	LoginMethod LoginMethod `gorm:"not null"` // "google" or "workos"
}

// workosOrgMapping links a WorkOS organization to the habitat org whose
// members it vouches for (see LoginMethodWorkOS).
type workosOrgMapping struct {
	WorkOSOrgID string     `gorm:"column:workos_org_id;primaryKey"` // e.g. "org_01H..."
	OrgDID      syntax.DID `gorm:"column:org_did;not null"`
}

// memberEmail records which DID a given work email was provisioned as,
// within its org. Populated once, at first sign-in.
type memberEmail struct {
	Email  Email      `gorm:"primaryKey"`
	OrgDID syntax.DID `gorm:"column:org_did;not null"`
	DID    syntax.DID `gorm:"column:did;not null;uniqueIndex"`
}

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) (*Store, error) {
	return &Store{db: db}, nil
}

// WithTx returns a copy of the store whose operations run on tx.
func (s *Store) WithTx(tx *gorm.DB) *Store {
	return &Store{db: tx}
}

func (s *Store) LookupDomain(
	ctx context.Context,
	domain string,
) (syntax.DID, LoginMethod, bool, error) {
	var row domainMapping
	err := s.db.WithContext(ctx).
		Where("domain = ?", strings.ToLower(domain)).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("lookup domain: %w", err)
	}
	return row.OrgDID, row.LoginMethod, true, nil
}

func (s *Store) GetDID(ctx context.Context, email Email) (syntax.DID, bool, error) {
	var row memberEmail
	err := s.db.WithContext(ctx).Where("email = ?", email).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get did by email: %w", err)
	}
	return row.DID, true, nil
}

func (s *Store) GetEmail(ctx context.Context, did syntax.DID) (Email, bool, error) {
	var row memberEmail
	err := s.db.WithContext(ctx).Where("did = ?", did).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get email by did: %w", err)
	}
	return row.Email, true, nil
}

// GetOrgDID returns the org did was provisioned into via email sign-in; ok
// is false for any DID not provisioned that way. The org is empty for a DID
// provisioned unaffiliated that has not yet signed in.
func (s *Store) GetOrgDID(ctx context.Context, did syntax.DID) (syntax.DID, bool, error) {
	var row memberEmail
	err := s.db.WithContext(ctx).Where("did = ?", did).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get provisioned org: %w", err)
	}
	return row.OrgDID, true, nil
}

// GetLoginMethod returns the login method of the org did was provisioned
// into via email sign-in; ok is false for any DID not provisioned that way.
// If the org has several mapped domains, any one's method is returned —
// a WorkOS-mapped org's domains all use "workos".
func (s *Store) GetLoginMethod(
	ctx context.Context,
	did syntax.DID,
) (LoginMethod, bool, error) {
	var rows []memberEmail
	if err := s.db.WithContext(ctx).Where("did = ?", did).Limit(1).Find(&rows).Error; err != nil {
		return "", false, fmt.Errorf("get login method: %w", err)
	}
	if len(rows) == 0 {
		return "", false, nil
	}
	var methods []LoginMethod
	if err := s.db.WithContext(ctx).
		Model(&domainMapping{}).
		Where("org_did = ?", rows[0].OrgDID).
		Limit(1).
		Pluck("login_method", &methods).Error; err != nil {
		return "", false, fmt.Errorf("get login method: %w", err)
	}
	if len(methods) == 0 {
		// Members with no domain-mapped org were placed by WorkOS sign-in.
		return LoginMethodWorkOS, true, nil
	}
	return methods[0], true, nil
}

// Provision records that email was provisioned as did in orgDID. It returns
// ErrEmailProvisioned if email already has a DID.
func (s *Store) Provision(
	ctx context.Context,
	email Email,
	orgDID, did syntax.DID,
) error {
	err := s.db.WithContext(ctx).Create(&memberEmail{
		Email:  email,
		OrgDID: orgDID,
		DID:    did,
	}).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrEmailProvisioned
	}
	if err != nil {
		return fmt.Errorf("provision email: %w", err)
	}
	return nil
}

// CreateDomainMapping maps domain to orgDID. It returns ErrDomainTaken if
// domain is already mapped to an org.
func (s *Store) CreateDomainMapping(
	ctx context.Context,
	domain string,
	orgDID syntax.DID,
	method LoginMethod,
) error {
	err := s.db.WithContext(ctx).Create(&domainMapping{
		Domain:      strings.ToLower(domain),
		OrgDID:      orgDID,
		LoginMethod: method,
	}).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrDomainTaken
	}
	if err != nil {
		return fmt.Errorf("create domain mapping: %w", err)
	}
	return nil
}

// CreateWorkOSOrgMapping maps workosOrgID to orgDID. It returns
// ErrWorkOSOrgTaken if workosOrgID is already mapped to an org.
func (s *Store) CreateWorkOSOrgMapping(
	ctx context.Context,
	workosOrgID string,
	orgDID syntax.DID,
) error {
	err := s.db.WithContext(ctx).Create(&workosOrgMapping{
		WorkOSOrgID: workosOrgID,
		OrgDID:      orgDID,
	}).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrWorkOSOrgTaken
	}
	if err != nil {
		return fmt.Errorf("create workos org mapping: %w", err)
	}
	return nil
}

// HasWorkOSOrg reports whether orgDID is mapped to any of workosOrgIDs,
// i.e. whether a user belonging to those WorkOS organizations belongs to
// orgDID.
func (s *Store) HasWorkOSOrg(
	ctx context.Context,
	orgDID syntax.DID,
	workosOrgIDs []string,
) (bool, error) {
	if len(workosOrgIDs) == 0 {
		return false, nil
	}
	var n int64
	if err := s.db.WithContext(ctx).
		Model(&workosOrgMapping{}).
		Where("org_did = ? AND workos_org_id IN ?", orgDID, workosOrgIDs).
		Count(&n).Error; err != nil {
		return false, fmt.Errorf("check workos org mapping: %w", err)
	}
	return n > 0, nil
}

// OrgHasWorkOSOrg reports whether orgDID is mapped to any WorkOS organization.
func (s *Store) OrgHasWorkOSOrg(ctx context.Context, orgDID syntax.DID) (bool, error) {
	var n int64
	if err := s.db.WithContext(ctx).
		Model(&workosOrgMapping{}).
		Where("org_did = ?", orgDID).
		Count(&n).Error; err != nil {
		return false, fmt.Errorf("check workos org mapping: %w", err)
	}
	return n > 0, nil
}

// LookupWorkOSOrg returns the org mapped to the first of workosOrgIDs that
// has a mapping; ok is false if none does.
func (s *Store) LookupWorkOSOrg(
	ctx context.Context,
	workosOrgIDs []string,
) (syntax.DID, bool, error) {
	if len(workosOrgIDs) == 0 {
		return "", false, nil
	}
	var rows []workosOrgMapping
	if err := s.db.WithContext(ctx).
		Where("workos_org_id IN ?", workosOrgIDs).
		Find(&rows).Error; err != nil {
		return "", false, fmt.Errorf("lookup workos org: %w", err)
	}
	// Honor the caller's ordering rather than the database's.
	for _, id := range workosOrgIDs {
		for _, r := range rows {
			if r.WorkOSOrgID == id {
				return r.OrgDID, true, nil
			}
		}
	}
	return "", false, nil
}

// SetMemberOrg records that did, provisioned unaffiliated (see
// Store.Provision with an empty org), now belongs to orgDID. It is a no-op
// for a DID that already has an org.
func (s *Store) SetMemberOrg(ctx context.Context, did, orgDID syntax.DID) error {
	if err := s.db.WithContext(ctx).
		Model(&memberEmail{}).
		Where("did = ? AND org_did = ''", did).
		Update("org_did", orgDID).Error; err != nil {
		return fmt.Errorf("set member org: %w", err)
	}
	return nil
}

// Models returns the GORM models this package persists. Their tables are
// created by db.Migrate.
func Models() []any {
	return []any{&domainMapping{}, &memberEmail{}, &workosOrgMapping{}}
}
