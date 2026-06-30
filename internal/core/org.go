package core

import (
	"context"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/db"
)

// Org represents a single organization on a pear instance.
type Org interface {
	DID() syntax.DID

	AddAdmin(ctx context.Context, admin syntax.DID) error
	GetAdmins(ctx context.Context) ([]syntax.DID, error)
	GetMembers(ctx context.Context) ([]syntax.DID, error)
	RemoveAdmin(ctx context.Context, admin syntax.DID) error
	RemoveMembers(ctx context.Context, members []syntax.DID) error
	DowngradeAdmin(ctx context.Context, admin syntax.DID) error
	IsAdmin(ctx context.Context, did syntax.DID) (bool, error)
	IsMember(ctx context.Context, did syntax.DID) (bool, error)

	GetMetadata(ctx context.Context, domain string) habitat.NetworkHabitatOrgGetMetadataOutput

	IssueIdentityToken(
		ctx context.Context,
		caller syntax.DID,
		reusable bool,
		expiresAt time.Time,
	) (token string, err error)
	CreateNewMemberIdentity(
		ctx context.Context,
		token string,
		internalHandle string,
		password string,
		loginID string,
	) (*identity.Identity, error)
	ValidateAdminSignedToken(ctx context.Context, token string) error

	db.Store[Org]
}
