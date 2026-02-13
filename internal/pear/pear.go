package pear

import (
	"context"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/inbox"
	"github.com/habitat-network/habitat/internal/permissions"
)

type Pear interface {
	PutRecord(caller syntax.DID, input habitat.NetworkHabitatRepoPutRecordInput) (habitat.NetworkHabitatRepoPutRecordOutput, error)
	GetRecord(habitat.NetworkHabitatRepoGetRecordParams) (habitat.NetworkHabitatRepoGetRecordOutput, error)
	ListRecords(habitat.NetworkHabitatRepoListRecordsParams) (habitat.NetworkHabitatRepoListRecordsOutput, error)
	UploadBlob([]byte) (habitat.NetworkHabitatRepoUploadBlobOutput, error)
	GetBlob(habitat.NetworkHabitatRepoGetBlobParams) ([]byte, error)
	AddPermission(habitat.NetworkHabitatPermissionsAddPermissionInput) error
	RemovePermission(habitat.NetworkHabitatPermissionsRemovePermissionInput) error
	ListPermissions(callerDID string) (habitat.NetworkHabitatPermissionsListPermissionsOutput, error)
}

// pear stands for Permission Enforcing ATProto Repo.
// This package implements that.

// The permissionEnforcingRepo wraps a repo, and enforces permissions on any calls.
type pear struct {
	ctx context.Context
	// The URL at which this repo lives; should match what is in a hosted user's DID doc for the habitat service entry
	url string
	// The service name for habitat in the DID doc (different for dev / production)
	serviceName string
	dir         identity.Directory

	// Backing for permissions
	permissions permissions.Store

	// The backing store for the data. Should implement similar methods to public atproto repos
	repo *repo

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
	perms permissions.Store,
	repo *repo,
	inbox inbox.Inbox,
) Pear {
	return &pear{
		ctx:         ctx,
		url:         "https://" + domain, // We use https
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
func (p *pear) putRecord(
	did string,
	collection string,
	record map[string]any,
	rkey string,
	validate *bool,
) error {
	// It is assumed right now that if this endpoint is called, the caller wants to put a private record into pear.
	return p.repo.putRecord(did, collection, rkey, record, validate)
}

func (p *pear) fetchDID(ctx context.Context, didOrHandle string) (syntax.DID, error) {
	// Try handling both handles and dids
	atid, err := syntax.ParseAtIdentifier(didOrHandle)
	if err != nil {
		return "", err
	}

	id, err := p.dir.Lookup(ctx, *atid)
	if err != nil {
		return "", err
	}
	return id.DID, nil
}

func (p *pear) PutRecord(ctx context.Context, caller syntax.DID, input habitat.NetworkHabitatRepoPutRecordInput) (habitat.NetworkHabitatRepoPutRecordOutput, error) {
	repoDID, err := p.fetchDID(ctx, input.Repo)
	if err != nil {
		return habitat.NetworkHabitatRepoPutRecordOutput{}, fmt.Errorf("parsing at identifier: %w", err)
	}

	if caller != repoDID {
		return habitat.NetworkHabitatRepoPutRecordOutput{}, fmt.Errorf("onlw owner can put record")
	}

	var rkey string
	if input.Rkey == "" {
		rkey = uuid.NewString()
	} else {
		rkey = input.Rkey
	}

	record, ok := input.Record.(map[string]any)
	if !ok {
		return habitat.NetworkHabitatRepoPutRecordOutput{}, fmt.Errorf("record must be a JSON object")
	}

	v := true
	validation, err := p.repo.putRecord(repoDID.String(), input.Collection, rkey, record, &v)
	if err != nil {
		return habitat.NetworkHabitatRepoPutRecordOutput{}, err
	}

	if len(input.Grantees) > 0 {
		err := p.permissions.AddReadPermission(
			input.Grantees,
			repoDID.String(),
			input.Collection+"."+rkey,
		)
		if err != nil {
			return habitat.NetworkHabitatRepoPutRecordOutput{}, err
		}
	}

	status := "unknown"
	if validation {
		status = "valid"
	}
	return habitat.NetworkHabitatRepoPutRecordOutput{
		Uri:              fmt.Sprintf("habitat://%s/%s/%s", repoDID.String(), input.Collection, rkey),
		ValidationStatus: status,
	}, nil
}

// getRecord checks permissions on callerDID and then passes through to `repo.getRecord`.
func (p *pear) getRecord(
	collection string,
	rkey string,
	targetDID syntax.DID,
	callerDID syntax.DID,
) (*Record, error) {
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

	return p.repo.getRecord(string(targetDID), collection, rkey)
}

func (p *pear) listRecords(
	params *habitat.NetworkHabitatRepoListRecordsParams,
	callerDID syntax.DID,
) ([]Record, error) {
	has, err := p.hasRepoForDid(syntax.DID(params.Repo))
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotLocalRepo
	}

	allow, deny, err := p.permissions.ListReadPermissionsByUser(
		params.Repo,
		callerDID.String(),
		params.Collection,
	)
	if err != nil {
		return nil, err
	}

	return p.repo.listRecords(params, allow, deny)
}

var (
	ErrNoHabitatServer = errors.New("no habitat server found for did :%s")
)

func (p *pear) hasRepoForDid(did syntax.DID) (bool, error) {
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
func (p *pear) getBlob(
	did string,
	cid string,
) (string /* mimetype */, []byte /* raw blob */, error) {
	return p.repo.getBlob(did, cid)
}

// TODO: actually enforce permissions here
func (p *pear) uploadBlob(did string, data []byte, mimeType string) (*blob, error) {
	return p.repo.uploadBlob(did, data, mimeType)
}

func (p *pear) notifyOfUpdate(ctx context.Context, sender syntax.DID, recipient syntax.DID, collection string, rkey string) error {
	return p.inbox.PutNotification(ctx, sender, recipient, collection, rkey)
}
