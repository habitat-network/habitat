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

// putRecord puts the given record on the repo connected to this permissionEnforcingRepo.
// It does not do any encryption, permissions, auth, etc. It is assumed that only the owner of the store can call this and that
// is gated by some higher up level. This should be re-written in the future to not give any incorrect impression.
func (p *Pear) putRecord(
	did string,
	collection string,
	record map[string]any,
	rkey string,
	validate *bool,
) error {
	// It is assumed right now that if this endpoint is called, the caller wants to put a private record into pear.
	return p.repo.PutRecord(did, collection, rkey, record, validate)
}

// getRecord checks permissions on callerDID and then passes through to `repo.getRecord`.
func (p *Pear) getRecord(
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

	return p.repo.GetRecord(string(targetDID), collection, rkey)
}

func (p *Pear) listRecords(
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

	return p.repo.ListRecords(did.String(), collection, allow, deny)
}

var (
	ErrNoHabitatServer = errors.New("no habitat server found for did :%s")
)

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

// TODO: actually enforce permissions here
func (p *Pear) getBlob(
	did string,
	cid string,
) (string /* mimetype */, []byte /* raw blob */, error) {
	return p.repo.GetBlob(did, cid)
}

// TODO: actually enforce permissions here
func (p *Pear) uploadBlob(did string, data []byte, mimeType string) (*repo.BlobRef, error) {
	return p.repo.UploadBlob(did, data, mimeType)
}

func (p *Pear) notifyOfUpdate(ctx context.Context, sender syntax.DID, recipient syntax.DID, collection string, rkey string, clique *string) error {
	return p.inbox.Put(ctx, sender, recipient, collection, rkey, clique)
}

func (p *Pear) addCliqueItem(ctx context.Context, cliqueURI habitat_syntax.HabitatURI, itemURI habitat_syntax.HabitatURI) error {
	cliqueDID, err := cliqueURI.Authority().AsDID()
	if err != nil {
		return err
	}
	// If the clique does not exist on this pear node, then forward the request to the clique's repo.
	ok, err := p.hasRepoForDid(cliqueDID)
	if err != nil {
		return err
	}

	did, collection, rkey, err := itemURI.ExtractParts()
	if err != nil {
		return err
	}
	if !ok {
		update := &habitat.NetworkHabitatInternalNotifyOfUpdateInput{
			Clique:     string(cliqueURI),
			Collection: collection.String(),
			Recipient:  string(cliqueDID),
			Rkey:       rkey.String(),
		}
		buf := new(bytes.Buffer)
		err := json.NewEncoder(buf).Encode(update)
		if err != nil {
			return err
		}

		req, err := http.NewRequest(http.MethodPost, "/xrpc/network.habitat.notifyOfUpdate", buf)
		_, err = p.xrpcCh.SendXRPC(
			ctx,
			did,
			cliqueDID,
			req,
		)
		return nil
	}

	// Otherwise, ensure that this item is indexed by the clique owner.
	// If the clique + item owners are the same, no need to take action since the item is indexed by way of the clique repo.
	if did == cliqueDID {
		// Nothing to do, return.
		return nil
	}

	// Otherwise, ensure that that the item is discoverable by the clique owner via the inbox.
	p.notifyOfUpdate(ctx, did, cliqueDID, collection.String(), rkey.String(), (*string)(&cliqueURI))
	return nil
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
