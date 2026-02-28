package pear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/inbox"
	"github.com/habitat-network/habitat/internal/node"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/repo"
	"github.com/rs/zerolog/log"

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
		ctx context.Context,
		caller syntax.DID,
		grantees []permissions.Grantee,
		owner syntax.DID,
		collection syntax.NSID,
		rkey syntax.RecordKey,
	) ([]permissions.Grantee, error)
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
	GetBlob(ctx context.Context, caller syntax.DID, target syntax.DID, cid syntax.CID) (string /* mimetype */, []byte /* raw blob */, error)
	UploadBlob(ctx context.Context, caller syntax.DID, target syntax.DID, data []byte, mimeType string) (*repo.BlobRef, error)

	// Inbox / Node-to-node communication related methods
	NotifyOfUpdate(ctx context.Context, sender syntax.DID, recipient syntax.DID, collection syntax.NSID, rkey syntax.RecordKey, reason string) error
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
	ctx context.Context,
	caller syntax.DID,
	grantees []permissions.Grantee,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) ([]permissions.Grantee, error) {
	// Authz: only the owner can modify a permission
	if caller != owner {
		return nil, ErrUnauthorized
	}

	didGrantees := []permissions.DIDGrantee{}
	cliqueGrantees := []permissions.CliqueGrantee{}
	for _, grantee := range grantees {
		switch g := grantee.(type) {
		case permissions.CliqueGrantee:
			cliqueGrantees = append(cliqueGrantees, g)
		case permissions.DIDGrantee:
			didGrantees = append(didGrantees, g)
		}
	}

	if collection == permissions.CliqueNSID && len(cliqueGrantees) > 0 {
		// Can't grant a clique permission to another clique
		return nil, ErrNoNestedCliques
	} else if collection == permissions.CliqueNSID && rkey == syntax.RecordKey("") {
		return nil, fmt.Errorf("adding permissions to the clique collection is not supported right now")
	}

	added, err := p.permissions.AddPermissions(grantees, owner, collection, rkey)
	if err != nil {
		return nil, err
	}

	// If clique:
	// This is the owner
	// Get all records with this permission from both inbox + repo
	// notify of update to all the added people
	if collection == permissions.CliqueNSID {
		// This gets everyone in the clique
		clique := habitat_syntax.ConstructHabitatUri(owner.String(), permissions.CliqueNSID.String(), rkey.String())

		// TODO: get al
		perms, err := p.permissions.ListCliquePermissions(ctx, permissions.CliqueGrantee(clique))
		if err != nil {
			return nil, err
		}

		records, err := p.repo.ListRecords(ctx, perms)
		if err != nil {
			return nil, err
		}

		notifs, err := p.inbox.GetUpdatesForClique(ctx, owner, clique.String())
		if err != nil {
			return nil, err
		}

		for _, grantee := range added {
			didGrantee, ok := grantee.(permissions.DIDGrantee)
			if !ok {
				return nil, fmt.Errorf("something went wrong")
			}

			serves, err := p.node.ServesDID(ctx, didGrantee.DID())
			if err != nil {
				return nil, err
			}

			for _, notif := range notifs {
				err := p.NotifyOfUpdate(ctx, syntax.DID(notif.Sender), didGrantee.DID(), syntax.NSID(notif.Collection), syntax.RecordKey(notif.Rkey), clique.String())
				if err != nil {
					return nil, err
				}
			}

			if serves {
				// Only notify about inbox notifs
				continue
			}

			for _, record := range records {
				err := p.NotifyOfUpdate(ctx, syntax.DID(record.Did), didGrantee.DID(), syntax.NSID(record.Collection), syntax.RecordKey(record.Rkey), "added_permission")
				if err != nil {
					return nil, err
				}
			}
		}
		return added, nil
	}

	// Otherwise, adding permissions for a specific record or collection
	if rkey == "" {
		// If it's a whole collection, we need to notify the new grantees about all the records for this collection that
		// they might care about. TODO: should we remove rkey from notification ?
		return nil, fmt.Errorf("adding permissions for a whole collection does not notify grantees yet")
	}

	// Otherwise, it's a specific record, this is the easiest case
	for _, grantee := range added {
		switch g := grantee.(type) {
		case permissions.CliqueGrantee:
			// TODO: Notify the clique owner of an update to the clique; they are responsible for delivering updates to clique members.
			// For now, since anyone in the clique can query all members in the clique, directly fan out
			dids, err := p.resolveClique(ctx, caller, g.Owner(), g.RecordKey())
			if errors.Is(err, ErrUnauthorized) {
				continue
			} else if err != nil {
				return nil, err
			}
			for _, grant := range dids {
				serves, err := p.node.ServesDID(ctx, grant.DID())
				if err != nil { // TODO: this could accidentally drop updates
					return nil, fmt.Errorf("resolving did host for grantee: %w", err)
				}

				// This DID will get updates via ListRecords on this PDS
				if serves {
					continue
				}

				if err = p.NotifyOfUpdate(ctx, owner, grant.DID(), collection, rkey, g.String()); err != nil {
					return nil, err
				}
			}
		case permissions.DIDGrantee:
			// TODO: if rkey == "", fetch all the relevant records
			err := p.NotifyOfUpdate(ctx, owner, g.DID(), collection, rkey, "added_permission")
			if err != nil {
				return nil, err
			}
		}
	}
	return added, nil
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
// Only returns non-ErrUnAuthorized errors
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
		return false, nil
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
	grantees []permissions.Grantee, // TODO: if the record already exists does this overwrite or add additional grantees?
) (habitat_syntax.HabitatURI, error) {
	// Basic authz check -- you can only write to your own repo.
	if target != caller {
		return "", fmt.Errorf("only owner can put record")
	}

	// Cliques in habitat are treated specially, they are a way to delegate permissions to a particular did, which requires some
	// special handling and coordination.
	if collection == permissions.CliqueNSID {
		// Followers are special cased to derive from bsky followers
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
		_, err := p.permissions.AddPermissions(
			grantees,
			did,
			collection,
			rkey,
		)
		if err != nil {
			return "", fmt.Errorf("adding permissions %w", err)
		}
	}

	uri, err := p.repo.PutRecord(ctx, repo.Record{
		Did:        did.String(),
		Collection: collection.String(),
		Rkey:       rkey.String(),
		Value:      record,
	}, validate)
	if err != nil {
		return "", fmt.Errorf("putting record: %w", err)
	}

	// TODO: if someone is added to the clique, fan out notifications about all the records currently in the clique
	// For now, ignore.
	if collection == permissions.CliqueNSID {
		return uri, nil
	}
	// Notify grantees of an update to a record they care about (for non-cliques)
	allGrantees, err := p.permissions.ListAllowedGranteesForRecord(ctx, did, collection, rkey)
	if err != nil {
		return "", fmt.Errorf("getting all grantees for record: %w", err)
	}

	for _, grantee := range allGrantees {
		switch g := grantee.(type) {
		case permissions.DIDGrantee:
			serves, err := p.node.ServesDID(ctx, g.DID())
			if err != nil { // TODO: this could accidentally drop updates
				return "", fmt.Errorf("resolving did host for grantee: %w", err)
			}

			// This DID will get updates via ListRecords on this PDS
			if serves {
				continue
			}

			// Otherwise, directly notify this grantee that there is an update to a record they care about
			if err = p.NotifyOfUpdate(ctx, did, g.DID(), collection, rkey, ""); err != nil {
				return "", err
			}

		case permissions.CliqueGrantee:
			// TODO: Notify the clique owner of an update to the clique; they are responsible for delivering updates to clique members.
			// For now, since anyone in the clique can query all members in the clique, directly fan out
			dids, err := p.resolveClique(ctx, did, g.Owner(), g.RecordKey())
			if errors.Is(err, ErrUnauthorized) {
				continue
			} else if err != nil {
				return "", err
			}
			for _, grant := range dids {
				serves, err := p.node.ServesDID(ctx, grant.DID())
				if err != nil { // TODO: this could accidentally drop updates
					return "", fmt.Errorf("resolving did host for grantee: %w", err)
				}

				// This DID will get updates via ListRecords on this PDS
				if serves {
					continue
				}

				if err = p.NotifyOfUpdate(ctx, did, grant.DID(), collection, rkey, g.String()); err != nil {
					return "", err
				}
			}
		}
	}

	return uri, nil
}

func (p *pear) resolveClique(
	ctx context.Context,
	caller syntax.DID,
	owner syntax.DID,
	rkey syntax.RecordKey,
) ([]permissions.DIDGrantee, error) {
	serves, err := p.node.ServesDID(ctx, owner)
	if err != nil {
		return nil, err
	}

	var grantees []permissions.Grantee

	if serves {
		grantees, err = p.ListAllowGrantsForRecord(ctx, caller, owner, permissions.CliqueNSID, rkey)
		if err != nil {
			return nil, err
		}
	} else {
		_, perms, err := p.getRecordRemote(ctx, caller, permissions.CliqueNSID, rkey, owner)
		if err != nil {
			return nil, err
		}
		grantees, err = permissions.ParseGranteesFromInterface(perms)
	}

	didGrantees := []permissions.DIDGrantee{}
	for _, grantee := range grantees {
		did, ok := grantee.(permissions.DIDGrantee)
		if !ok {
			log.Err(fmt.Errorf("unexpected nested clique: remote clique has clique grantes"))
		} else {
			didGrantees = append(didGrantees, did)
		}
	}

	if len(grantees) != len(didGrantees) {
		log.Err(fmt.Errorf("unexpected nested clique: remote clique has clique grantes"))
	}

	return append(didGrantees, permissions.DIDGrantee(owner) /* The owner is always part of the clique */), nil
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

func (p *pear) getRecordRemote(
	ctx context.Context,
	caller syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
	target syntax.DID,
) (*repo.Record, []interface{} /* permissions */, error) {
	// Otherwise, forward this request to the right repo (the clique member)
	reqURL, err := url.Parse("/xrpc/network.habitat.getRecord")
	if err != nil {
		return nil, nil, err
	}
	q := reqURL.Query()
	q.Set("repo", target.String())
	q.Set("collection", collection.String())
	q.Set("rkey", rkey.String())
	reqURL.RawQuery = q.Encode()
	if err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		reqURL.String(),
		nil,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("constructing http request for remote resolve: %w", err)
	}

	resp, err := p.node.SendXRPC(ctx, caller, target, req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		var rec habitat.NetworkHabitatRepoGetRecordOutput
		err := json.NewDecoder(resp.Body).Decode(&rec)
		if err != nil {
			return nil, nil, err
		}

		val, ok := rec.Value.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("unexpected record value type: %T", rec.Value)
		}

		return &repo.Record{
			Did:        target.String(),
			Collection: collection.String(),
			Rkey:       rkey.String(),
			Value:      val,
		}, rec.Permissions, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, nil, ErrUnauthorized
	default:
		return nil, nil, fmt.Errorf("unexpected status from remote getRecord: %d", resp.StatusCode)
	}
}

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

	rec, _, err := p.getRecordRemote(ctx, caller, collection, rkey, target)
	return rec, err
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

func (p *pear) resolveRecordsFromNotifications(ctx context.Context, caller syntax.DID, notifs []inbox.Notification) ([]repo.Record, error) {
	records := []repo.Record{}
	for _, notif := range notifs {
		record, _, err := p.getRecordRemote(ctx, caller, syntax.NSID(notif.Collection), syntax.RecordKey(notif.Rkey), syntax.DID(notif.Sender))
		if errors.Is(err, ErrUnauthorized) {
			continue
		}
		if err != nil {
			return nil, err
		}
		records = append(records, *record)
	}
	return records, nil
}

func (p *pear) listRecordsRemote(ctx context.Context, caller syntax.DID, collection syntax.NSID) ([]repo.Record, error) {
	// All the remote record this caller cares about can be resolved via the inbox
	notifs, err := p.inbox.GetCollectionUpdatesByRecipient(ctx, caller, collection)
	if err != nil {
		return nil, err
	}

	return p.resolveRecordsFromNotifications(ctx, caller, notifs)
}

// This needs to be renamed
func (p *pear) ListRecords(ctx context.Context, caller syntax.DID, collection syntax.NSID, subjects []syntax.DID) ([]repo.Record, error) {
	// Get records owned by this repo
	localRecords, err := p.listRecordsLocal(ctx, collection, caller, subjects)
	if err != nil {
		return nil, err
	}

	// TODO: implement
	remoteRecords, err := p.listRecordsRemote(ctx, caller, collection)
	if err != nil {
		return nil, err
	}

	return append(localRecords, remoteRecords...), nil
}

// TODO: actually enforce permissions here
func (p *pear) GetBlob(
	ctx context.Context,
	caller syntax.DID,
	target syntax.DID,
	cid syntax.CID,
) (string /* mimetype */, []byte /* raw blob */, error) {
	serves, err := p.node.ServesDID(ctx, target)
	if err != nil {
		return "", nil, err
	}

	// TODO: implement this via xrpc forwarding
	if !serves {
		return "", nil, ErrRemoteFetchUnsupported
	}
	authz := false

	if caller == target {
		authz = true
	} else {
		links, err := p.repo.GetBlobLinks(ctx, cid, target)
		if err != nil {
			return "", nil, err
		}

		// TODO: could be done in parallel
		for _, uri := range links {
			ok, err := p.HasPermission(ctx, caller, caller, uri.Authority().DID(), uri.Collection(), uri.RecordKey())
			if err != nil {
				return "", nil, err
			}

			// If any record the caller has access to references this blob, they can see it
			if ok {
				authz = true
				break
			}
		}
	}

	if !authz {
		return "", nil, ErrUnauthorized
	}

	return p.repo.GetBlob(ctx, target.String(), cid.String())
}

func (p *pear) UploadBlob(ctx context.Context, caller syntax.DID, target syntax.DID, data []byte, mimeType string) (*repo.BlobRef, error) {
	// You can only upload blobs to your own repo
	if caller != target {
		return nil, ErrUnauthorized
	}
	return p.repo.UploadBlob(ctx, target.String(), data, mimeType)
}

func (p *pear) NotifyOfUpdate(
	ctx context.Context,
	sender syntax.DID,
	recipient syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
	reason string,
) error {
	serves, err := p.node.ServesDID(ctx, recipient)
	if err != nil {
		return err
	}

	if serves {
		return p.inbox.Put(ctx, sender, recipient, collection, rkey, reason)
	}

	reqURL, err := url.Parse("/xrpc/network.habitat.internal.notifyOfUpdate")
	input := habitat.NetworkHabitatInternalNotifyOfUpdateInput{
		Collection: collection.String(),
		Recipient:  recipient.String(),
		Rkey:       rkey.String(),
	}

	marshalled, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshalling notifyOfUpdate input: %w", err)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		reqURL.String(),
		bytes.NewReader(marshalled),
	)

	_, err = p.node.SendXRPC(ctx, sender, recipient, req)
	return err
}
