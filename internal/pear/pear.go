package pear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/inbox"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/repo"
	"github.com/habitat-network/habitat/internal/xrpcchannel"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// pear stands for Permission Enforcing ATProto Repo.
// This package implements that.

// The permissionEnforcingRepo wraps a repo, and enforces permissions on any calls.
type Pear struct {
	ctx context.Context
	// The URL at which this repo lives; should match what is in a hosted user's DID doc for the habitat service entry
	url string
	// The service name for habitat in the DID doc (different for dev / production)
	serviceName string
	dir         identity.Directory

	// Channel on which to forward xrpc requests to other pear nodes.
	xrpcCh xrpcchannel.XrpcChannel

	// Backing for permissions
	permissions permissions.Store

	// The backing store for the data. Should implement similar methods to public atproto repos
	repo repo.Repo

	// Manage receiving updates for records (replacement for the Firehose)
	inbox inbox.Inbox
}

var (
	ErrPublicRecordExists      = fmt.Errorf("a public record exists with the same key")
	ErrNoPutsOnEncryptedRecord = fmt.Errorf("directly put-ting to this lexicon is not valid")
	ErrNotLocalRepo            = fmt.Errorf("the desired did does not live on this repo")
	ErrUnauthorized            = fmt.Errorf("unauthorized request")
	ErrNoHabitatServer         = errors.New("no habitat server found for did :%s")
)

func NewPear(
	ctx context.Context,
	domain string,
	serviceName string,
	dir identity.Directory,
	xrpcCh xrpcchannel.XrpcChannel,
	perms permissions.Store,
	repo repo.Repo,
	inbox inbox.Inbox,
) *Pear {
	return &Pear{
		ctx:         ctx,
		url:         "https://" + domain, // We use https
		serviceName: serviceName,
		dir:         dir,
		xrpcCh:      xrpcCh,
		permissions: perms,
		repo:        repo,
		inbox:       inbox,
	}
}

// Helpers
func (p *Pear) hasRepoForDid(did syntax.DID) (bool, error) {
	id, err := p.dir.LookupDID(p.ctx, did)
	if err != nil {
		return false, err
	}

	found, ok := id.Services[p.serviceName]
	if !ok {
		return false, fmt.Errorf(ErrNoHabitatServer.Error(), did.String())
	}

	return found.URL == p.url, nil
}

// putRecord puts the given record on the repo connected to this permissionEnforcingRepo.
// It does not do any encryption, permissions, auth, etc. It is assumed that only the owner of the store can call this and that
// is gated by some higher up level. This should be re-written in the future to not give any incorrect impression.
func (p *Pear) putRecord(
	ctx context.Context,
	did string,
	collection string,
	record map[string]any,
	rkey string,
	validate *bool,
	grantees []grantee,
) (habitat_syntax.HabitatURI, error) {
	// It is assumed right now that if this endpoint is called, the caller wants to put a private record into pear.
	uri, err := p.repo.PutRecord(ctx, did, collection, rkey, record, validate)
	if err != nil {
		return "", err
	}

	didGrantees := []string{}
	cliqueGrantees := []string{}
	for _, grantee := range grantees {
		switch g := grantee.(type) {
		case didGrantee:
			didGrantees = append(didGrantees, string(g))
		case cliqueGrantee:
			// For clique grantees, we need to notify the clique owner that there is a new record to be aware of
			cliqueGrantees = append(cliqueGrantees, string(g))
			return "", fmt.Errorf("clique grantees are not supported yet")
		}
	}

	err = p.permissions.AddReadPermission(
		append(didGrantees, cliqueGrantees...),
		did,
		collection+"."+rkey,
	)
	if err != nil {
		return "", err
	}

	if len(cliqueGrantees) == 0 {
		return "", nil
	}

	// If we granted permission to a clique, we need to notify the clique owner.
	for _, clique := range cliqueGrantees {
		cliqueURI, err := habitat_syntax.ParseHabitatURI(clique)
		if err != nil {
			return "", err
		}
		p.addCliqueItem(ctx, cliqueURI, uri)
	}
	return uri, nil
}

// getRecord checks permissions on callerDID and then passes through to `repo.getRecord`.
func (p *Pear) getRecord(
	ctx context.Context,
	collection string,
	rkey string,
	targetDID syntax.DID,
	callerDID syntax.DID,
) (*repo.Record, error) {
	has, err := p.hasRepoForDid(targetDID)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotLocalRepo
	}

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

	return p.repo.GetRecord(ctx, string(targetDID), collection, rkey)
}

func (p *Pear) listRecords(
	ctx context.Context,
	did syntax.DID,
	collection string,
	callerDID syntax.DID,
) ([]repo.Record, error) {
	has, err := p.hasRepoForDid(did)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotLocalRepo
	}

	allow, deny, err := p.permissions.ListReadPermissionsByUser(
		did.String(),
		callerDID.String(),
		collection,
	)
	if err != nil {
		return nil, err
	}

	return p.repo.ListRecords(ctx, did.String(), collection, allow, deny)
}

// Blob-related methods
// TODO: actually enforce permissions here
func (p *Pear) getBlob(
	ctx context.Context,
	did string,
	cid string,
) (string /* mimetype */, []byte /* raw blob */, error) {
	return p.repo.GetBlob(ctx, did, cid)
}

// TODO: actually enforce permissions here
func (p *Pear) uploadBlob(ctx context.Context, did string, data []byte, mimeType string) (*repo.BlobRef, error) {
	return p.repo.UploadBlob(ctx, did, data, mimeType)
}

// Inbox-related methods
func (p *Pear) notifyOfUpdate(ctx context.Context, sender syntax.DID, recipient syntax.DID, collection string, rkey string, clique *string) error {
	has, err := p.hasRepoForDid(recipient)
	if err != nil {
		return err
	}

	sendUpdateToOtherRepo := func(update *habitat.NetworkHabitatInternalNotifyOfUpdateInput) error {
		if clique != nil {
			update.Clique = *clique
		}
		buf := new(bytes.Buffer)
		err = json.NewEncoder(buf).Encode(update)
		if err != nil {
			return err
		}

		req, err := http.NewRequest(http.MethodPost, "/xrpc/network.habitat.notifyOfUpdate", buf)
		if err != nil {
			return err
		}
		_, err = p.xrpcCh.SendXRPC(
			ctx,
			sender,
			recipient,
			req,
		)
		return err
	}

	if has {
		// TODO: if the notification is on a clique that this recipient owns, ensure that the sender is part of the clique.
		if clique != nil {
			uri, err := habitat_syntax.ParseHabitatURI(*clique)
			if err != nil {
				return fmt.Errorf("malformed clique parameter: %s", uri)
			}
			did, err := uri.Authority().AsDID()
			if err != nil {
				return fmt.Errorf("unable to extract did from clique parameter: %s", uri)
			}

			// If this record is a clique root, and the recipient owns it, then the recipient need to fan out notifications.
			// (This is the fan in from all clique members to the clique owner)
			if did == recipient {
				// Optimization: we can skip putting into the recipients inbox if the sender and recipient are on the same node, since listRecords for the recipient will
				// fetch any record on this pear node the recipient has access to, even if it isn't theirs.
				if hasSender, err := p.hasRepoForDid(sender); err == nil && hasSender {
					// No need to call inbox.Put(...)
				} else {
					err := p.inbox.Put(ctx, sender, recipient, collection, rkey, clique)
					if err != nil {
						return fmt.Errorf("putting in inbox: %w", err)
					}
				}

				cliqueMembers, err := p.permissions.ListGranteesForRecord(ctx, recipient.String(), collection, rkey)
				if err != nil {
					return fmt.Errorf("getting clique members: %w", err)
				}

				for _, member := range cliqueMembers {
					cliqueStr := uri.String()
					if did, err := syntax.ParseDID(member); err == nil {
						// TODO: is this weird? recursion.
						p.notifyOfUpdate(ctx, recipient, syntax.DID(did.String()), collection, rkey, &cliqueStr)
					} else {
						// We should never have allowed nested cliques.
						return fmt.Errorf("found a nested clique -- should never happen")
					}
				}

			} else if did != sender {
				// Otherwise, this recipient should only accept clique notifications from the clique owner.
				// (This is the fan out from clique owner to all clique members)
				return ErrUnauthorized
			}
		}
		return p.inbox.Put(ctx, sender, recipient, collection, rkey, clique)
	}

	// TODO: if the notification was for a record part of a clique, fan out that notification to all the clique owners.

	// If the recipient does not exist on this node, forward the request.
	return sendUpdateToOtherRepo(
		&habitat.NetworkHabitatInternalNotifyOfUpdateInput{
			Collection: collection,
			Recipient:  recipient.String(),
			Rkey:       rkey,
		},
	)
}

// Clique-related methods
func (p *Pear) addCliqueItem(ctx context.Context, cliqueURI habitat_syntax.HabitatURI, itemURI habitat_syntax.HabitatURI) error {
	cliqueDID, err := cliqueURI.Authority().AsDID()
	if err != nil {
		return err
	}

	did, collection, rkey, err := itemURI.ExtractParts()
	if err != nil {
		return err
	}

	// If the clique + item owners are the same, no need to take action since the item is indexed by way of the clique repo.
	if did == cliqueDID {
		// Nothing to do, return.
		return nil
	}

	// Otherwise, ensure that that the item is discoverable by the clique owner via the inbox.
	return p.notifyOfUpdate(ctx, did, cliqueDID, collection.String(), rkey.String(), (*string)(&cliqueURI))
}

func (p *Pear) getCliqueItems(ctx context.Context, cliqueURI habitat_syntax.HabitatURI) ([]habitat_syntax.HabitatURI, error) {
	cliqueDID, err := cliqueURI.Authority().AsDID()
	if err != nil {
		return nil, err
	}

	has, err := p.hasRepoForDid(cliqueDID)
	if err != nil {
		return nil, err
	}

	// If the clique does not exist on this repo, then forward the request.
	if !has {
		// TODO: unimplemented -- not sure when this would happen
	}

	// Otherwise, the clique exists on this repo, so fetch relevant rkeys from both the inbox and the repo of the owner did.
	inboxURIs, err := p.inbox.GetCliqueItems(p.ctx, cliqueDID.String(), string(cliqueURI))
	if err != nil {
		return nil, err
	}

	// Get the list of records that are granted the clique permissions from the permission store
	uris, err := p.permissions.ListAllowedRecordsByGrantee(ctx, cliqueDID.String(), string(cliqueURI))
	if err != nil {
		return nil, err
	}

	return append(inboxURIs, uris...), nil
}

// Permissions-related methods

// TODO: understand if this works with rkey = ""
func (p *Pear) addReadPermission(ctx context.Context, grantee grantee, caller string, collection string, rkey string) error {
	fmt.Println("add read permission", grantee, caller, collection+rkey)
	cliqueGrantee, isCliqueGrantee := grantee.(cliqueGrantee)
	if isCliqueGrantee && rkey == "" {
		// If it's a clique, ensure that rkey is populated (can't grant a clique permission to a whole collection -- unsupported for now)
		return fmt.Errorf("granting a clique permissions to an entire collection is unsupported")
	}

	err := p.permissions.AddReadPermission(
		[]string{grantee.String()},
		caller,
		collection+rkey, // TODO: seperate these when the permission store is fixed
	)
	if err != nil {
		return fmt.Errorf("adding read permission: %w", err)
	}

	// There are some cases that need further work.
	if !isCliqueGrantee {
		// If we are granting permission directly to a did grantee then always notify them that there is something new to see.
		err := p.notifyOfUpdate(ctx, syntax.DID(caller), syntax.DID(grantee.String()), collection, rkey, nil)
		if err != nil {
			return fmt.Errorf("notify new grantees of an update: %w", err)
		}

		// If the record we are granting permission on happens to be a clique root, then this inbox + repo contains all notifications for this clique. Look those up and fan them out.
		maybeClique := habitat_syntax.ConstructHabitatUri(caller, collection, rkey)
		items, err := p.getCliqueItems(ctx, maybeClique)
		if err != nil {
			return fmt.Errorf("getting clique items: %w", err)
		}

		maybeCliqueStr := string(maybeClique)
		for _, itemURI := range items {
			_, collection, rkey, err := itemURI.ExtractParts()
			err = p.notifyOfUpdate(ctx, syntax.DID(caller), syntax.DID(grantee.String()), collection.String(), rkey.String(), &maybeCliqueStr)
			if err != nil {
				return fmt.Errorf("clique owner, notify clique members of an update: %w", err)
			}
		}
		return nil
	}

	// Otherwise, if the grantee is a clique, then notify the clique owner that there is an update to forward.
	uri, err := habitat_syntax.ParseHabitatURI(cliqueGrantee.String())
	if err != nil {
		return fmt.Errorf("parsing habitat URI: %w", err)
	}

	cliqueDID, err := uri.Authority().AsDID()
	if err != nil {
		return fmt.Errorf("resolving clique did: %w", err)
	}

	clique := cliqueGrantee.String()
	fmt.Println("down here")
	return p.notifyOfUpdate(ctx, syntax.DID(caller), cliqueDID, collection, rkey, &clique)
}
