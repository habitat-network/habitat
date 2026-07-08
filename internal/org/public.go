package org

import (
	"context"
	"errors"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"gorm.io/gorm"
)

var ErrNotSupportedPublic = errors.New("method not supported on public org")

// Create an org that has everyone for instances that don't belong to an org, they just host pear servers for people.
type EveryoneOrg struct{}

// DID implements [Org].
func (e *EveryoneOrg) DID() syntax.DID {
	// TODO: actually serve the did doc for this
	return syntax.DID("did:web:public.habitat.network")
}

func (e *EveryoneOrg) LoginMethod(ctx context.Context) LoginMethod {
	return LoginMethodAtproto
}

// GetMetadata implements Org.
func (e *EveryoneOrg) GetMetadata(
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
func (e *EveryoneOrg) AddAdmin(ctx context.Context, admin syntax.DID) error {
	return ErrNotSupportedPublic
}

// AddMembers implements Org.
func (e *EveryoneOrg) AddMembers(ctx context.Context, members []syntax.DID) error {
	return ErrNotSupportedPublic
}

// GetAdmins implements Org.
func (e *EveryoneOrg) GetAdmins(ctx context.Context) ([]syntax.DID, error) {
	return nil, ErrNotSupportedPublic
}

// GetMembers implements Org.
func (e *EveryoneOrg) GetMembers(ctx context.Context) ([]syntax.DID, error) {
	return nil, ErrNotSupportedPublic
}

// IsAdmin implements Org.
func (e *EveryoneOrg) IsAdmin(ctx context.Context, did syntax.DID) (bool, error) {
	return false, ErrNotSupportedPublic
}

// RemoveAdmin implements Org.
func (e *EveryoneOrg) RemoveAdmin(ctx context.Context, admin syntax.DID) error {
	return ErrNotSupportedPublic
}

// DowngradeAdmin implements Org.
func (e *EveryoneOrg) DowngradeAdmin(ctx context.Context, admin syntax.DID) error {
	return ErrNotSupportedPublic
}

// RemoveMembers implements Org.
func (e *EveryoneOrg) RemoveMembers(ctx context.Context, members []syntax.DID) error {
	return ErrNotSupportedPublic
}

// IsMember implements Store.
func (e *EveryoneOrg) IsMember(ctx context.Context, member syntax.DID) (bool, error) {
	// Everyone is a member of the everyone org
	return true, nil
}

// AuthenticateMember implements Org.
func (e *EveryoneOrg) AuthenticateMember(
	ctx context.Context,
	handle string,
	password string,
) (bool, error) {
	return false, ErrNotSupportedPublic
}

// WithTx implements [Org].
func (e *EveryoneOrg) WithTx(tx *gorm.DB) Org {
	return e
}

func NewEveryoneOrg() Org {
	return &EveryoneOrg{}
}
