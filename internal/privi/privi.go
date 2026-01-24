package privi

import (
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/permissions"
)

// Privi is an ATProto PDS Wrapper which allows for storing & getting private data.
// It does this by encrypting data, then storing it in blob. A special lexicon for this purpose,
// identified by network.habitat.encryptedRecord, points to the blob storing the actual data.
//
// This encryption layer is transparent to the caller -- Privi.PutRecord() and Privi.GetRecord() have
// the same API has com.atproto.putRecord and com.atproto.getRecord.
//
// TODO: formally define the network.habitat.encryptedRecord and change it to a domain we actually own :)
type store struct {
	// TODO: consider encrypting at rest. We probably do not want to do this but do want to construct a wholly separate MST for private data.
	// e           Encrypter
	permissions permissions.Store

	// The backing store for the data. Should implement similar methods to public atproto repos
	repo *sqliteRepo
}

var (
	ErrPublicRecordExists      = fmt.Errorf("a public record exists with the same key")
	ErrNoPutsOnEncryptedRecord = fmt.Errorf("directly put-ting to this lexicon is not valid")
	ErrNotLocalRepo            = fmt.Errorf("the desired did does not live on this repo")
	ErrUnauthorized            = fmt.Errorf("unauthorized request")
)

// TODO: take in a carfile/sqlite where user's did is persisted
func newStore(perms permissions.Store, repo *sqliteRepo) *store {
	return &store{
		permissions: perms,
		repo:        repo,
	}
}

// putRecord puts the given record on the repo connected to this store (currently an in-memory repo that is a KV store)
// It does not do any encryption, permissions, auth, etc. It is assumed that only the owner of the store can call this and that
// is gated by some higher up level. This should be re-written in the future to not give any incorrect impression.
func (p *store) putRecord(
	did string,
	collection string,
	record map[string]any,
	rkey string,
	validate *bool,
) error {
	// It is assumed right now that if this endpoint is called, the caller wants to put a private record into privi.
	return p.repo.putRecord(did, fmt.Sprintf("%s.%s", collection, rkey), record, validate)
}

// getRecord checks permissions on callerDID and then passes through to `repo.getRecord`.
func (p *store) getRecord(
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

func (p *store) listRecords(
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

func (p *store) hasRepoForDid(did string) (bool, error) {
	has, err := p.repo.hasRepoForDid(did)
	if err != nil {
		return false, err
	}
	return has, nil
}
