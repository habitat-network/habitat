package pear

import (
	"context"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/inbox"
	"github.com/habitat-network/habitat/internal/node"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/repo"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Wrapper around the permission store / repository / inbox components that together make up a permission-enforcing atproto repository
// The contract this interface seeks to satisfy is that all methods can be safely called on any combination of inputs and permissions will be enforced, as long as the given
// DIDs are *authenticated*. In other wrods, this wrapper protects authorization of the repository.
// Every function in this interface likely needs to take in caller, the authenticated DID, in order to run authorization checks against them.
// Be VERY CAREFUL if you add a function that does not take in caller, as that
// could allow insufficient authz checking and leak data.
//
// This is the core of the habitat server.
type Pear interface {
	// Permissions-related methods
	HasPermission(
		ctx context.Context,
		caller syntax.DID,
		requester syntax.DID,
		owner syntax.DID,
		collection syntax.NSID,
		rkey syntax.RecordKey,
	) (bool, error)
	AddPermissions(
		caller syntax.DID,
		grantees []permissions.Grantee,
		owner syntax.DID,
		collection syntax.NSID,
		rkey syntax.RecordKey,
	) error
	RemovePermissions(
		caller syntax.DID,
		grantee []permissions.Grantee,
		owner syntax.DID,
		collection syntax.NSID,
		rkey syntax.RecordKey,
	) error
	ListPermissionGrants(
		ctx context.Context,
		caller syntax.DID,
		granter syntax.DID,
	) ([]permissions.Permission, error)
	ListAllowGrantsForRecord(
		ctx context.Context,
		caller syntax.DID,
		owner syntax.DID,
		collection syntax.NSID,
		rkey syntax.RecordKey,
	) ([]permissions.Grantee, error)

	// Repository methods; roughly analgous to com.atproto.repo methods
	PutRecord(ctx context.Context, caller, target syntax.DID, collection syntax.NSID, record map[string]any, rkey syntax.RecordKey, validate *bool, grantees []permissions.Grantee) (habitat_syntax.HabitatURI, error)
	GetRecord(ctx context.Context, collection syntax.NSID, rkey syntax.RecordKey, target syntax.DID, caller syntax.DID) (*repo.Record, error)
	ListRecords(ctx context.Context, caller syntax.DID, collection syntax.NSID, subjects []syntax.DID) ([]repo.Record, error)
	GetBlob(ctx context.Context, did syntax.DID, cid syntax.CID) (string /* mimetype */, []byte /* raw blob */, error)
	UploadBlob(ctx context.Context, did syntax.DID, data []byte, mimeType string) (*repo.BlobRef, error)

	// Inbox / Node-to-node communication related methods
	NotifyOfUpdate(ctx context.Context, sender syntax.DID, recipient syntax.DID, collection string, rkey string) error
}

// pear stands for Permission Enforcing ATProto Repo.
// This package implements that.

// The permissionEnforcingRepo wraps a repo, and enforces permissions on any calls.
type pear struct {
	dir identity.Directory

	// Context about where this pear lives and communicatino channel with other nodes
	node node.Node

	// Backing for permissions
	permissions permissions.Store

	// The backing store for the data. Should implement similar methods to public atproto repos
	repo repo.Repo

	// Manage receiving updates for records (replacement for the Firehose)
	inbox inbox.Inbox
}

// Pass throughs to implement permission.Store
func (p *pear) AddPermissions(
	caller syntax.DID,
	grantees []permissions.Grantee,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) error {
	// Authz: only the owner can modify a permission
	if caller != owner {
		return ErrUnauthorized
	}
	return p.permissions.AddPermissions(grantees, owner, collection, rkey)
}

// RemoveReadPermissions implements Pear.
func (p *pear) RemovePermissions(
	caller syntax.DID,
	grantee []permissions.Grantee,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) error {
	// Authz: only the owner can modify a permission
	if caller != owner {
		return ErrUnauthorized
	}
	return p.permissions.RemovePermissions(grantee, owner, collection, rkey)
}

// HasPermission implements Pear.
func (p *pear) HasPermission(
	ctx context.Context,
	caller syntax.DID,
	requester syntax.DID,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) (bool, error) {
	// Authz: if the caller has permission, the caller can see who else has permission
	// This can probably be optimized in terms of DB queries, but path of least resistance for now.
	callerOk, err := p.permissions.HasPermission(ctx, caller, owner, collection, rkey)
	if err != nil {
		return false, fmt.Errorf("[pear] error calling permissions.HasPermission: %w", err)
	}

	// If the caller doesn't have permission to this record, they can't see who does.
	if !callerOk {
		return false, ErrUnauthorized
	}
	return p.permissions.HasPermission(ctx, requester, owner, collection, rkey)
}

// ListPermissionGrants implements Pear.
func (p *pear) ListPermissionGrants(ctx context.Context, caller syntax.DID, granter syntax.DID) ([]permissions.Permission, error) {
	// Authz: only the granter can see this
	if caller != granter {
		return nil, ErrUnauthorized
	}
	return p.permissions.ListPermissionGrants(ctx, granter)
}

// ListPermissions implements Pear.
func (p *pear) ListAllowGrantsForRecord(ctx context.Context, caller syntax.DID, owner syntax.DID, collection syntax.NSID, rkey syntax.RecordKey) ([]permissions.Grantee, error) {
	// Authz: if the caller has permission, the caller can see who else has permission
	callerOk, err := p.permissions.HasPermission(ctx, caller, owner, collection, rkey)
	if err != nil {
		return nil, err
	}

	// If the caller doesn't have permission to this record, they can't see who does.
	if !callerOk {
		return nil, ErrUnauthorized
	}

	return p.permissions.ListAllowedGranteesForRecord(ctx, owner, collection, rkey)
}

var _ Pear = &pear{}

var (
	ErrPublicRecordExists     = fmt.Errorf("a public record exists with the same key")
	ErrNotLocalRepo           = fmt.Errorf("the desired did does not live on this repo")
	ErrUnauthorized           = fmt.Errorf("unauthorized request")
	ErrNoNestedCliques        = errors.New("nested cliques are not allowed")
	ErrFollowersCliqueRkey    = errors.New("this clique cannot be directly set, it derives from app.bsky.graph.follows of the user")
	ErrRemoteFetchUnsupported = errors.New("fetches from remote pears are unsupported as of now")
)

func NewPear(
	node node.Node,
	dir identity.Directory,
	perms permissions.Store,
	repo repo.Repo,
	inbox inbox.Inbox,
) *pear {
	return &pear{
		node:        node,
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
	caller syntax.DID,
	target syntax.DID,
	collection syntax.NSID,
	record map[string]any,
	rkey syntax.RecordKey,
	validate *bool,
	grantees []permissions.Grantee,
) (habitat_syntax.HabitatURI, error) {
	// Basic authz check -- you can only write to your own repo.
	if target != caller {
		return "", fmt.Errorf("only owner can put record")
	}

	// Cliques in habitat are treated specially, they are a way to delegate permissions to a particular did, which requires some
	// special handling and coordination.
	if collection == permissions.CliqueNSID {
		if rkey == permissions.FollowersCliqueRkey {
			return "", ErrFollowersCliqueRkey
		}
		for _, grantee := range grantees {
			if _, ok := grantee.(permissions.CliqueGrantee); ok {
				// No nested cliques allowed -- can't grant a clique permission to a clique
				return "", ErrNoNestedCliques
			}
		}
	}

	did := target
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

	return p.repo.PutRecord(ctx, repo.Record{
		Did:        did.String(),
		Collection: collection.String(),
		Rkey:       rkey.String(),
		Value:      record,
	}, validate)
}

func (p *pear) getRecordLocal(
	ctx context.Context,
	collection syntax.NSID,
	rkey syntax.RecordKey,
	target syntax.DID,
	caller syntax.DID,
) (*repo.Record, error) {
	ok, err := p.permissions.HasPermission(ctx, caller, target, collection, rkey)
	if err != nil {
		return nil, err
	}

	if !ok {
		return nil, ErrUnauthorized
	}

	return p.repo.GetRecord(ctx, target.String(), collection.String(), rkey.String())
}

/*
func (p *pear) getRecordRemote(
	ctx context.Context,
	collection syntax.NSID,
	rkey syntax.RecordKey,
	target syntax.DID,
	caller syntax.DID,
) (*repo.Record, error) {
	// Otherwise, forward this request to the right repo (the clique member)
	reqURL, err := url.Parse("/xrpc/network.habitat.getRecord")
	if err != nil {
		return nil, err
	}
	q := reqURL.Query()
	q.Set("repo", target.String())
	q.Set("collection", collection.String())
	q.Set("rkey", rkey.String())
	reqURL.RawQuery = q.Encode()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		reqURL.String(),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("constructing http request for remote resolve: %w", err)
	}

	resp, err := p.node.SendXRPC(ctx, caller, target, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		var rec habitat.NetworkHabitatRepoGetRecordOutput
		err := json.NewDecoder(resp.Body).Decode(&rec)
		if err != nil {
			return nil, err
		}

		bytes, err := json.Marshal(rec.Value)
		if err != nil {
			return nil, err
		}

		return &repo.Record{
			Did:        target.String(),
			Collection: collection.String(),
			Rkey:       rkey.String(),
			Value:      bytes,
		}, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrUnauthorized
	default:
		return nil, fmt.Errorf("unexpected status from remote getRecord: %d", resp.StatusCode)
	}
}
*/

// getRecord checks permissions on caller and then passes through to `repo.getRecord`.
func (p *pear) GetRecord(
	ctx context.Context,
	collection syntax.NSID,
	rkey syntax.RecordKey,
	target syntax.DID,
	caller syntax.DID,
) (*repo.Record, error) {
	ok, err := p.node.ServesDID(ctx, target)
	if err != nil {
		return nil, err
	}

	if ok {
		return p.getRecordLocal(ctx, collection, rkey, target, caller)
	}

	return nil, ErrRemoteFetchUnsupported
	// TODO: implement
	// return p.getRecordRemote(ctx, collection, rkey, target, caller)
}

// Remove once ListRecords() is implemented correctly. Separate so i can still read old code.
func (p *pear) listRecordsLocal(
	ctx context.Context,
	collection syntax.NSID,
	caller syntax.DID,
	subjects []syntax.DID,
) ([]repo.Record, error) {
	if collection == "" {
		return nil, fmt.Errorf("only support filtering by a collection")
	}

	perms, err := p.permissions.ResolvePermissionsForCollection(ctx, caller, collection, subjects)
	if err != nil {
		return nil, err
	}

	records, err := p.repo.ListRecords(ctx, perms)
	if err != nil {
		return nil, fmt.Errorf("failed to list records: %w", err)
	}
	return records, nil
}

/*
func (p *pear) listRecordsRemote(ctx context.Context, caller syntax.DID, collection syntax.NSID) ([]repo.Record, error) {
	// All the remote record this caller cares about can be resolved via the inbox
	notifs, err := p.inbox.GetCollectionUpdatesByRecipient(ctx, caller, collection)
	if err != nil {
		return nil, err
	}

	records := []repo.Record{}
	for _, notif := range notifs {
		record, err := p.getRecordRemote(ctx, collection, syntax.RecordKey(notif.Rkey), syntax.DID(notif.Sender), syntax.DID(notif.Recipient))
		if err != nil {
			return nil, err
		}
		records = append(records, *record)
	}
	return records, nil
}
*/

// This needs to be renamed
func (p *pear) ListRecords(ctx context.Context, caller syntax.DID, collection syntax.NSID, subjects []syntax.DID) ([]repo.Record, error) {
	// Get records owned by this repo
	localRecords, err := p.listRecordsLocal(ctx, collection, caller, subjects)
	if err != nil {
		return nil, err
	}

	/*
		// TODO: implement
		remoteRecords, err := p.listRecordsRemote(ctx, caller, collection)
		if err != nil {
			return nil, err
		}
	*/

	return localRecords, nil
}

// TODO: actually enforce permissions here
func (p *pear) GetBlob(
	ctx context.Context,
	did syntax.DID,
	cid syntax.CID,
) (string /* mimetype */, []byte /* raw blob */, error) {
	return p.repo.GetBlob(ctx, did.String(), cid.String())
}

// TODO: actually enforce permissions here
func (p *pear) UploadBlob(ctx context.Context, did syntax.DID, data []byte, mimeType string) (*repo.BlobRef, error) {
	return p.repo.UploadBlob(ctx, did.String(), data, mimeType)
}

func (p *pear) NotifyOfUpdate(
	ctx context.Context,
	sender syntax.DID,
	recipient syntax.DID,
	collection string,
	rkey string,
) error {
	return p.inbox.Put(ctx, sender, recipient, syntax.NSID(collection), rkey)
}
