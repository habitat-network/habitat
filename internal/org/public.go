package org

import (
	"context"
	"errors"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Create an org that has everyone for instances that don't belong to an org, they just host pear servers for people.
type everyoneOrg struct{}

var (
	ErrNotSupportedPublic = errors.New("method not supported on public org")
)

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

var _ Org = &everyoneOrg{}

func NewEveryoneOrg() Org {
	return &everyoneOrg{}
}
