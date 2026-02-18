package pear

import (
	"context"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/inbox"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/repo"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Wrapper around the permission store / repository / inbox components that together make up a permission-enforcing atproto repository
// The contract this interface seeks to satisfy is that all methods can be safely called on any combination of inputs and permissions will be enforced, as long as the given
// DIDs are *authenticated*. In other wrods, this wrapper protects authorization of the repository.
//
// This is the core of the habitat server.
type Pear interface {
	permissions.Store

	// Permissioned repository methods
	PutRecord(ctx context.Context, callerDID, targetDID, collection string, record map[string]any, rkey string, validate *bool, grantees []permissions.Grantee) (habitat_syntax.HabitatURI, error)
	GetRecord(ctx context.Context, collection, rkey string, targetDID syntax.DID, callerDID syntax.DID) (*repo.Record, error)
	ListRecords(ctx context.Context, did syntax.DID, collection string, callerDID syntax.DID) ([]repo.Record, error)
	GetBlob(ctx context.Context, did string, cid string) (string /* mimetype */, []byte /* raw blob */, error)
	UploadBlob(ctx context.Context, did string, data []byte, mimeType string) (*repo.BlobRef, error)

	// Inbox-related methods
	NotifyOfUpdate(ctx context.Context, sender syntax.DID, recipient syntax.DID, collection string, rkey string) error
}

// pear stands for Permission Enforcing ATProto Repo.
// This package implements that.

// The permissionEnforcingRepo wraps a repo, and enforces permissions on any calls.
type pear struct {
	// The URL at which this repo lives; should match what is in a hosted user's DID doc for the habitat service entry
	url string
	// The service name for habitat in the DID doc (different for dev / production)
	serviceName string
	dir         identity.Directory

	// Backing for permissions
	permissions permissions.Store

	// The backing store for the data. Should implement similar methods to public atproto repos
	repo repo.Repo

	// Manage receiving updates for records (replacement for the Firehose)
	inbox inbox.Inbox
}

// Pass throughs to implement permission.Store
func (p *pear) AddReadPermission(grantees []permissions.Grantee, owner string, collection string, rkey string) error {
	return p.permissions.AddReadPermission(grantees, owner, collection, rkey)
}

// HasPermission implements Pear.
func (p *pear) HasPermission(requester string, owner string, nsid string, rkey string) (bool, error) {
	return p.permissions.HasPermission(requester, owner, nsid, rkey)
}

// ListReadPermissionsByGrantee implements Pear.
func (p *pear) ListReadPermissionsByUser(grantee syntax.DID, collection string) ([]permissions.Permission, error) {
	return p.permissions.ListReadPermissionsByUser(grantee, collection)
}

// ListReadPermissionsByLexicon implements Pear.
func (p *pear) ListReadPermissionsByLexicon(owner string) (map[string][]string, error) {
	return p.permissions.ListReadPermissionsByLexicon(owner)
}

// RemoveReadPermissions implements Pear.
func (p *pear) RemoveReadPermissions(grantee []permissions.Grantee, owner string, collection string, rkey string) error {
	return p.permissions.RemoveReadPermissions(grantee, owner, collection, rkey)
}

var _ Pear = &pear{}

var (
	ErrPublicRecordExists      = fmt.Errorf("a public record exists with the same key")
	ErrNoPutsOnEncryptedRecord = fmt.Errorf("directly put-ting to this lexicon is not valid")
	ErrNotLocalRepo            = fmt.Errorf("the desired did does not live on this repo")
	ErrUnauthorized            = fmt.Errorf("unauthorized request")
)

func NewPear(
	serviceName string,
	serviceEndpoint string,
	dir identity.Directory,
	perms permissions.Store,
	repo repo.Repo,
	inbox inbox.Inbox,
) *pear {
	return &pear{
		url:         serviceEndpoint,
		serviceName: serviceName,
		dir:         dir,
		permissions: perms,
		repo:        repo,
		inbox:       inbox,
	}
}

// putRecord puts the given record on the repo connected to this permissionEnforcingRepo.
// It does not do any encryption, permissions, auth, etc. It is assumed that only the owner of the store can call this and that
// is gated by some higher up level. This should be re-written in the future to not give any incorrect impression.
func (p *pear) PutRecord(
	ctx context.Context,
	callerDID string,
	targetDID string,
	collection string,
	record map[string]any,
	rkey string,
	validate *bool,
	grantees []permissions.Grantee,
) (habitat_syntax.HabitatURI, error) {
	if targetDID != callerDID {
		return "", fmt.Errorf("only owner can put record")
	}

	did := targetDID
	// It is assumed right now that if this endpoint is called, the caller wants to put a private record into pear.
	if len(grantees) > 0 {
		err := p.permissions.AddReadPermission(
			grantees,
			did,
			collection,
			rkey,
		)
		if err != nil {
			return "", fmt.Errorf("adding permissions %w", err)
		}
	}

	return p.repo.PutRecord(ctx, did, collection, rkey, record, validate)
}

// getRecord checks permissions on callerDID and then passes through to `repo.getRecord`.
func (p *pear) GetRecord(
	ctx context.Context,
	collection string,
	rkey string,
	targetDID syntax.DID,
	callerDID syntax.DID,
) (*repo.Record, error) {
	// Run permissions before returning to the user
	authz, err := p.permissions.HasPermission(
		callerDID.String(),
		targetDID.String(),
		collection,
		rkey,
	)
	if err != nil {
		return nil, err
	}

	if !authz {
		return nil, ErrUnauthorized
	}

	// User has permission, return the record
	return p.repo.GetRecord(ctx, targetDID.String(), collection, rkey)
}

func (p *pear) ListRecords(
	ctx context.Context,
	did syntax.DID,
	collection string,
	callerDID syntax.DID,
) ([]repo.Record, error) {
	perms, err := p.permissions.ListReadPermissionsByUser(
		callerDID,
		collection,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list permissions: %w", err)
	}
	records, err := p.repo.ListRecords(ctx, perms)
	if err != nil {
		return nil, fmt.Errorf("failed to list records: %w", err)
	}
	return records, nil
}

var ErrNoHabitatServer = errors.New("no habitat server found for did :%s")

// Identity helpers
func (p *pear) hasRepoForDid(ctx context.Context, did syntax.DID) (bool, error) {
	id, err := p.dir.LookupDID(ctx, did)
	if err != nil {
		return false, err
	}

	found, ok := id.Services[p.serviceName]
	if !ok {
		return false, fmt.Errorf(ErrNoHabitatServer.Error(), did.String())
	}

	return found.URL == p.url, nil
}

// TODO: actually enforce permissions here
func (p *pear) GetBlob(
	ctx context.Context,
	did string,
	cid string,
) (string /* mimetype */, []byte /* raw blob */, error) {
	return p.repo.GetBlob(ctx, did, cid)
}

// TODO: actually enforce permissions here
func (p *pear) UploadBlob(ctx context.Context, did string, data []byte, mimeType string) (*repo.BlobRef, error) {
	return p.repo.UploadBlob(ctx, did, data, mimeType)
}

func (p *pear) NotifyOfUpdate(
	ctx context.Context,
	sender syntax.DID,
	recipient syntax.DID,
	collection string,
	rkey string,
) error {
	return p.inbox.PutNotification(ctx, sender, recipient, collection, rkey)
}
