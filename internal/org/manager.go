package org

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/pkg/org"
)

// org.Manager is used to manage the organization tied to a pear node
// It enforces permissions on its methods; i.e non-admins cannot add admins or members.
//
// Eventually we can add management scopes to member vs. admin roles, to allow members to add other members etc.
type Manager interface {
	BootstrapAdmin(ctx context.Context, bootstrapSecret string, admin syntax.DID) error
	GetAdmins(ctx context.Context, caller syntax.DID) ([]syntax.DID, error)
	GetMembers(ctx context.Context, caller syntax.DID) ([]syntax.DID, error)
	AddAdmin(ctx context.Context, actor syntax.DID, admin syntax.DID) error
	AddMembers(ctx context.Context, actor syntax.DID, members []syntax.DID) error
	RemoveAdmin(ctx context.Context, actor syntax.DID, admin syntax.DID)
	RemoveMembers(ctx context.Context, actor syntax.DID, members []syntax.DID) error
}

type manager struct {
	org   org.Org
	store store
}
