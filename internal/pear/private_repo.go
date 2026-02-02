package pear

import (
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/permissions"
)

// pear stands for Permission Enforcing ATProto Repo.
// This package implements that.

// The permissionEnforcingRepo wraps a repo, and enforces permissions on any calls.
type permissionEnforcingRepo struct {
	permissions permissions.Store

	// The backing store for the data. Should implement similar methods to public atproto repos
	repo *repo
}

var (
	ErrPublicRecordExists = fmt.Errorf("a public record exists with the same key")
	ErrNotLocalRepo       = fmt.Errorf("the desired did does not live on this repo")
	ErrUnauthorized       = fmt.Errorf("unauthorized request")
)

func newPermissionEnforcingRepo(perms permissions.Store, repo *repo) *permissionEnforcingRepo {
	return &permissionEnforcingRepo{
		permissions: perms,
		repo:        repo,
	}
}

// putRecord puts the given record on the repo connected to this permissionEnforcingRepo.
// It does not do any encryption, permissions, auth, etc. It is assumed that only the owner of the store can call this and that
// is gated by some higher up level. This should be re-written in the future to not give any incorrect impression.
func (p *permissionEnforcingRepo) putRecord(
	did string,
	collection string,
	record map[string]any,
	rkey string,
	validate *bool,
) error {
	// It is assumed right now that if this endpoint is called, the caller wants to put a private record into pear.
	return p.repo.putRecord(did, fmt.Sprintf("%s.%s", collection, rkey), record, validate)
}

// getRecord checks permissions on callerDID and then passes through to `repo.getRecord`.
func (p *permissionEnforcingRepo) getRecord(
	collection string,
	rkey string,
	targetDID syntax.DID,
	callerDID syntax.DID,
) (*Record, error) {
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

	return p.repo.getRecord(string(targetDID), fmt.Sprintf("%s.%s", collection, rkey))
}

func (p *permissionEnforcingRepo) listRecords(
	params *habitat.NetworkHabitatRepoListRecordsParams,
	callerDID syntax.DID,
) ([]Record, error) {
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

func (p *permissionEnforcingRepo) hasRepoForDid(did string) (bool, error) {
	has, err := p.repo.hasRepoForDid(did)
	if err != nil {
		return false, err
	}
	return has, nil
}

// TODO: actually enforce permissions here
func (p *permissionEnforcingRepo) getBlob(
	did string,
	cid string,
) (string /* mimetype */, []byte /* raw blob */, error) {
	return p.repo.getBlob(did, cid)
}

// TODO: actually enforce permissions here
func (p *permissionEnforcingRepo) uploadBlob(did string, data []byte, mimeType string) (*blob, error) {
	return p.repo.uploadBlob(did, data, mimeType)
}
