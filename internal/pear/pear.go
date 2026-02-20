package pear

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/inbox"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/repo"
	"github.com/habitat-network/habitat/internal/xrpcchannel"

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
	PutRecord(ctx context.Context, callerDID, targetDID syntax.DID, collection syntax.NSID, record map[string]any, rkey syntax.RecordKey, validate *bool, grantees []permissions.Grantee) (habitat_syntax.HabitatURI, error)
	GetRecord(ctx context.Context, collection syntax.NSID, rkey syntax.RecordKey, targetDID syntax.DID, callerDID syntax.DID) (*repo.Record, error)
	ListRecords(ctx context.Context, targetDIDs []syntax.DID, targetCollections []syntax.NSID, callerDID syntax.DID) ([]repo.Record, error)
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

	// Channel to talk to other pear nodes
	xrpcCh xrpcchannel.XrpcChannel

	// Backing for permissions
	permissions permissions.Store

	// The backing store for the data. Should implement similar methods to public atproto repos
	repo repo.Repo

	// Manage receiving updates for records (replacement for the Firehose)
	inbox inbox.Inbox
}

// Pass throughs to implement permission.Store
func (p *pear) AddPermissions(
	grantees []permissions.Grantee,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) error {
	return p.permissions.AddPermissions(grantees, owner, collection, rkey)
}

// HasPermission implements Pear.
func (p *pear) HasDirectPermission(
	requester syntax.DID,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) (bool, error) {
	return p.permissions.HasDirectPermission(requester, owner, collection, rkey)
}

// ListReadPermissions implements Pear.
func (p *pear) ListPermissions(
	grantee syntax.DID,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) ([]permissions.Permission, error) {
	return p.permissions.ListPermissions(grantee, owner, collection, rkey)
}

// RemoveReadPermissions implements Pear.
func (p *pear) RemovePermissions(
	grantee []permissions.Grantee,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) error {
	return p.permissions.RemovePermissions(grantee, owner, collection, rkey)
}

var _ Pear = &pear{}

var (
	ErrPublicRecordExists      = fmt.Errorf("a public record exists with the same key")
	ErrNoPutsOnEncryptedRecord = fmt.Errorf("directly put-ting to this lexicon is not valid")
	ErrNotLocalRepo            = fmt.Errorf("the desired did does not live on this repo")
	ErrUnauthorized            = fmt.Errorf("unauthorized request")
	ErrNoHabitatServer         = errors.New("no habitat server found for did :%s")
	ErrNoNestedCliques         = errors.New("nested cliques are not allowed")
)

func NewPear(
	serviceName string,
	serviceEndpoint string,
	dir identity.Directory,
	xrpcCh xrpcchannel.XrpcChannel,
	perms permissions.Store,
	repo repo.Repo,
	inbox inbox.Inbox,
) *pear {
	return &pear{
		url:         serviceEndpoint,
		serviceName: serviceName,
		dir:         dir,
		xrpcCh:      xrpcCh,
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
	callerDID syntax.DID,
	targetDID syntax.DID,
	collection syntax.NSID,
	record map[string]any,
	rkey syntax.RecordKey,
	validate *bool,
	grantees []permissions.Grantee,
) (habitat_syntax.HabitatURI, error) {
	// Basic authz check -- you can only write to your own repo.
	if targetDID != callerDID {
		return "", fmt.Errorf("only owner can put record")
	}

	// Cliques in habitat are treated specially, they are a way to delegate permissions to a particular did, which requires some
	// special handling and coordination.
	if collection == permissions.CliqueNSID {
		for _, grantee := range grantees {
			if _, ok := grantee.(permissions.CliqueGrantee); ok {
				// No nested cliques allowed -- can't grant a clique permission to a clique
				return "", ErrNoNestedCliques
			}
		}
	}

	did := targetDID
	// It is assumed right now that if this endpoint is called, the caller wants to put a private record into pear.
	if len(grantees) > 0 {
		err := p.permissions.AddPermissions(
			grantees,
			did,
			collection,
			rkey,
		)
		if err != nil {
			return "", fmt.Errorf("adding permissions %w", err)
		}
	}

	return p.repo.PutRecord(ctx, did.String(), collection.String(), rkey.String(), record, validate)
}

func (p *pear) isCliqueMember(ctx context.Context, did syntax.DID, clique permissions.CliqueGrantee) (bool, error) {
	uri := habitat_syntax.HabitatURI(clique)
	owner, err := uri.Authority().AsDID()
	if err != nil {
		return false, fmt.Errorf("malformed clique authority: %w", err)
	}
	ok, err := p.hasRepoForDid(ctx, owner)
	if err != nil {
		return false, err
	}

	if ok {
		return p.permissions.HasDirectPermission(did, owner, uri.Collection(), uri.RecordKey())
	}

	// Otherwise, forward this request to the right repo (the clique member)
	reqURL, err := url.Parse("/xrpc/network.habitat.getRecord")
	if err != nil {
		return false, err
	}
	q := reqURL.Query()
	q.Set("repo", owner.String())
	q.Set("collection", uri.Collection().String())
	q.Set("rkey", uri.RecordKey().String())
	reqURL.RawQuery = q.Encode()
	if err != nil {
		return false, err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		reqURL.String(),
		nil,
	)
	if err != nil {
		return false, fmt.Errorf("constructing http request: %w", err)
	}

	resp, err := p.xrpcCh.SendXRPC(ctx, did, owner, req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected status from remote getRecord: %d", resp.StatusCode)
	}
}

// getRecord checks permissions on callerDID and then passes through to `repo.getRecord`.
func (p *pear) GetRecord(
	ctx context.Context,
	collection syntax.NSID,
	rkey syntax.RecordKey,
	targetDID syntax.DID,
	callerDID syntax.DID,
) (*repo.Record, error) {
	perms, err := p.permissions.ListPermissions(callerDID, targetDID, collection, rkey)
	if err != nil {
		return nil, err
	}

	// Try to find an explicit allow or deny permission
	cliquesToResolve := []permissions.CliqueGrantee{}
	for _, perm := range perms {
		switch g := perm.Grantee.(type) {
		case permissions.DIDGrantee:
			if perm.Grantee.String() != callerDID.String() {
				// Should never happen
				return nil, fmt.Errorf("unexpected: found a permission that does not apply to the given query: %v", perm)
			}
			// Explicity deny -- return unauthorized
			if perm.Effect == permissions.Deny {
				return nil, ErrUnauthorized
			}
			// Explicit allow -- return the record
			return p.repo.GetRecord(ctx, targetDID.String(), collection.String(), rkey.String())
		case permissions.CliqueGrantee:
			cliquesToResolve = append(cliquesToResolve, g)
		}
	}

	// Otherwise, resolve returned clique grantees to see if a valid permission exists.
	// TODO: could do these in a batched / parallel way.
	for _, clique := range cliquesToResolve {
		ok, err := p.isCliqueMember(ctx, callerDID, clique)
		if err != nil {
			return nil, fmt.Errorf("resolving clique membership: %w", err)
		} else if !ok {
			return nil, ErrUnauthorized
		} else {
			// User has permission, return the record
			return p.repo.GetRecord(ctx, targetDID.String(), collection.String(), rkey.String())
		}
	}

	// Default: no relevant permission grants or cliques found; unauthorized.
	return nil, ErrUnauthorized
}

func (p *pear) ListRecords(
	ctx context.Context,
	targetDIDs []syntax.DID,
	targetCollections []syntax.NSID,
	callerDID syntax.DID,
) ([]repo.Record, error) {

	perms := []permissions.Permission{}
	for _, target := range targetDIDs {
		for _, collection := range targetCollections {
			p, err := p.permissions.ListPermissions(
				callerDID,
				target,
				collection,
				"", // search for all records in this collection
			)
			if err != nil {
				return nil, fmt.Errorf("failed to list permissions: %w", err)
			}

			perms = append(perms, p...)
		}
	}

	records, err := p.repo.ListRecords(ctx, perms)
	if err != nil {
		return nil, fmt.Errorf("failed to list records: %w", err)
	}
	return records, nil
}

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
