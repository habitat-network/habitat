package org

import (
	"context"
	"errors"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/core"
	"gorm.io/gorm"
)

var ErrNotSupportedPublic = errors.New("method not supported on public org")

// Create an org that has everyone for instances that don't belong to an org, they just host pear servers for people.
type everyoneOrg struct{}

// DID implements [Org].
func (e *everyoneOrg) DID() syntax.DID {
	// TODO: actually serve the did doc for this
	return syntax.DID("did:web:public.habitat.network")
}

// ValidateAdminSignedToken implements [Org].
func (e *everyoneOrg) ValidateAdminSignedToken(ctx context.Context, token string) error {
	return ErrNotSupportedPublic
}

func (e *everyoneOrg) loginMethod(ctx context.Context) loginMethod {
	return LoginMethodAtproto
}

// GetMetadata implements Org.
func (e *everyoneOrg) GetMetadata(
	_ context.Context,
	domain string,
) habitat.NetworkHabitatOrgGetMetadataOutput {
	return habitat.NetworkHabitatOrgGetMetadataOutput{
		Description: "the default org everyone with a personal account belongs to; no-op",
		LoginMethod: "at protocol",
		Name:        "default organization",
	}
}

// AddAdmin implements Org.
func (e *everyoneOrg) AddAdmin(ctx context.Context, admin syntax.DID) error {
	return ErrNotSupportedPublic
}

// AddMembers implements Org.
func (e *everyoneOrg) AddMembers(ctx context.Context, members []syntax.DID) error {
	return ErrNotSupportedPublic
}

// GetAdmins implements Org.
func (e *everyoneOrg) GetAdmins(ctx context.Context) ([]syntax.DID, error) {
	return nil, ErrNotSupportedPublic
}

// GetMembers implements Org.
func (e *everyoneOrg) GetMembers(ctx context.Context) ([]syntax.DID, error) {
	return nil, ErrNotSupportedPublic
}

// IsAdmin implements Org.
func (e *everyoneOrg) IsAdmin(ctx context.Context, did syntax.DID) (bool, error) {
	return false, ErrNotSupportedPublic
}

// RemoveAdmin implements Org.
func (e *everyoneOrg) RemoveAdmin(ctx context.Context, admin syntax.DID) error {
	return ErrNotSupportedPublic
}

// DowngradeAdmin implements Org.
func (e *everyoneOrg) DowngradeAdmin(ctx context.Context, admin syntax.DID) error {
	return ErrNotSupportedPublic
}

// RemoveMembers implements Org.
func (e *everyoneOrg) RemoveMembers(ctx context.Context, members []syntax.DID) error {
	return ErrNotSupportedPublic
}

// IsMember implements Store.
func (e *everyoneOrg) IsMember(ctx context.Context, member syntax.DID) (bool, error) {
	// Everyone is a member of the everyone org
	return true, nil
}

// IssueIdentityToken implements Org.
func (e *everyoneOrg) IssueIdentityToken(
	ctx context.Context,
	caller syntax.DID,
	reusable bool,
	expiresAt time.Time,
) (token string, err error) {
	return "", ErrNotSupportedPublic
}

// CreateNewMemberIdentity implements Org.
func (e *everyoneOrg) CreateNewMemberIdentity(
	ctx context.Context,
	token string,
	internalHandle string,
	password string,
	loginID string,
) (*identity.Identity, error) {
	return nil, ErrNotSupportedPublic
}

// AuthenticateMember implements Org.
func (e *everyoneOrg) AuthenticateMember(
	ctx context.Context,
	handle string,
	password string,
) (bool, error) {
	return false, ErrNotSupportedPublic
}

// WithTx implements [Org].
func (e *everyoneOrg) WithTx(tx *gorm.DB) core.Org {
	return e
}

func NewEveryoneOrg() core.Org {
	return &everyoneOrg{}
}
