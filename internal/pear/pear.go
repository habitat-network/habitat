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

// pear stands for Permission Enforcing ATProto Repo.
// This package implements that.

// The permissionEnforcingRepo wraps a repo, and enforces permissions on any calls.
type Pear struct {
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
) *Pear {
	return &Pear{
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
func (p *Pear) putRecord(
	ctx context.Context,
	did string,
	collection string,
	record map[string]any,
	rkey string,
	validate *bool,
) (habitat_syntax.HabitatURI, error) {
	// It is assumed right now that if this endpoint is called, the caller wants to put a private record into pear.
	return p.repo.PutRecord(ctx, did, collection, rkey, record, validate)
}

// getRecord checks permissions on callerDID and then passes through to `repo.getRecord`.
func (p *Pear) getRecord(
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

func (p *Pear) listRecords(
	ctx context.Context,
	did syntax.DID,
	collection string,
	callerDID syntax.DID,
) ([]repo.Record, error) {
	var allRecords []repo.Record

	// Step 1: Get records from caller's own repo
	allow, deny, err := p.permissions.ListReadPermissionsByUser(
		did.String(),
		callerDID.String(),
		collection,
	)
	if err != nil {
		return nil, err
	}

	ownRecords, err := p.repo.ListRecords(ctx, callerDID.String(), collection, allow, deny)
	if err != nil {
		return nil, err
	}
	allRecords = append(allRecords, ownRecords...)

	// Step 2: Query permissions store to get two lists:
	// 1. Users with full collection access
	// 2. Specific records with direct permissions
	fullAccessOwners, specificRecords, err := p.permissions.ListCrossRepoAccessByCollection(callerDID.String(), collection)
	if err != nil {
		return nil, err
	}

	// Step 3: Query all records for owners with full collection access in a single query
	if len(fullAccessOwners) > 0 {
		ownerRecords, err := p.repo.ListRecordsByOwnersDeprecated(fullAccessOwners, collection)
		if err != nil {
			return nil, err
		}
		allRecords = append(allRecords, ownerRecords...)
	}

	// Step 4: Query all specific records in a single query
	if len(specificRecords) > 0 {
		recordPairs := make([]struct {
			Owner string
			Rkey  string
		}, len(specificRecords))
		for i, recordPerm := range specificRecords {
			recordPairs[i] = struct {
				Owner string
				Rkey  string
			}{Owner: recordPerm.Owner, Rkey: recordPerm.Rkey}
		}
		specificRecordsResult, err := p.repo.ListSpecificRecordsDeprecated(collection, recordPairs)
		if err != nil {
			return nil, err
		}
		allRecords = append(allRecords, specificRecordsResult...)
	}

	return allRecords, nil
}

var (
	ErrNoHabitatServer = errors.New("no habitat server found for did :%s")
)

func (p *Pear) hasRepoForDid(ctx context.Context, did syntax.DID) (bool, error) {
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

func (p *Pear) notifyOfUpdate(ctx context.Context, sender syntax.DID, recipient syntax.DID, collection string, rkey string) error {
	return p.inbox.PutNotification(ctx, sender, recipient, collection, rkey)
}
