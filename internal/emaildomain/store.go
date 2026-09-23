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
)

// domainMapping routes an email domain to the org that owns it and the
// login method members of that domain use.
type domainMapping struct {
	Domain      string      `gorm:"primaryKey"` // e.g. "acme.com"
	OrgDID      syntax.DID  `gorm:"column:org_did;not null"`
	LoginMethod LoginMethod `gorm:"not null"` // "google" (only value today)
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
	if err := db.AutoMigrate(&domainMapping{}, &memberEmail{}); err != nil {
		return nil, fmt.Errorf("automigrate: %w", err)
	}
	return &Store{db: db}, nil
}

// WithTx returns a copy of the store whose operations run on tx.
func (s *Store) WithTx(tx *gorm.DB) *Store {
	return &Store{db: tx}
}

// Transaction runs fn in a transaction on this store's DB, so callers can
// scope this and other stores (via their WithTx) to the same transaction.
func (s *Store) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return s.db.WithContext(ctx).Transaction(fn)
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

// GetLoginMethod returns the login method of the org did was provisioned
// into via email sign-in; ok is false for any DID not provisioned that way.
// If the org has several mapped domains, any one's method is returned —
// they're all "google" today.
func (s *Store) GetLoginMethod(
	ctx context.Context,
	did syntax.DID,
) (LoginMethod, bool, error) {
	var methods []LoginMethod
	if err := s.db.WithContext(ctx).
		Model(&memberEmail{}).
		Joins("JOIN domain_mappings ON domain_mappings.org_did = member_emails.org_did").
		Where("member_emails.did = ?", did).
		Limit(1).
		Pluck("domain_mappings.login_method", &methods).Error; err != nil {
		return "", false, fmt.Errorf("get login method: %w", err)
	}
	if len(methods) == 0 {
		return "", false, nil
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
