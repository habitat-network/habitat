package privi

import (
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/eagraf/habitat-new/internal/permissions"
)

type record map[string]any

// A record key is a string
type recordKey string

// Privi is an ATProto PDS Wrapper which allows for storing & getting private data.
// It does this by encrypting data, then storing it in blob. A special lexicon for this purpose,
// identified by com.habitat.encryptedRecord, points to the blob storing the actual data.
//
// This encryption layer is transparent to the caller -- Privi.PutRecord() and Privi.GetRecord() have
// the same API has com.atproto.putRecord and com.atproto.getRecord.
//
// TODO: formally define the com.habitat.encryptedRecord and change it to a domain we actually own :)
type store struct {
	did syntax.DID
	// TODO: consider encrypting at rest. We probably do not want to do this but do want to construct a wholly separate MST for private data.
	// e           Encrypter
	permissions permissions.Store

	// The backing store for the data. Should implement similar methods to public atproto repos
	repo repo
}

var (
	ErrPublicRecordExists      = fmt.Errorf("a public record exists with the same key")
	ErrNoPutsOnEncryptedRecord = fmt.Errorf("directly put-ting to this lexicon is not valid")
	ErrNotLocalRepo            = fmt.Errorf("the desired did does not live on this repo")
	ErrUnauthorized            = fmt.Errorf("unauthorized request")
)

// TODO: take in a carfile/sqlite where user's did is persisted
func newStore(did syntax.DID, perms permissions.Store, repo repo) *store {
	return &store{
		did:         did,
		permissions: perms,
		repo:        repo,
	}
}

// putRecord puts the given record on the repo connected to this store (currently an in-memory repo that is a KV store)
// It does not do any encryption, permissions, auth, etc. It is assumed that only the owner of the store can call this and that
// is gated by some higher up level. This should be re-written in the future to not give any incorrect impression.
func (p *store) putRecord(did string, collection string, record map[string]any, rkey string, validate *bool) error {
	// It is assumed right now that if this endpoint is called, the caller wants to put a private record into privi.
	return p.repo.putRecord(did, rkey, record, validate)
}

type GetRecordResponse struct {
	Cid   *string `json:"cid"`
	Uri   string  `json:"uri"`
	Value any     `json:"value"`
}

// getRecord checks permissions on callerDID and then passes through to `repo.getRecord`.
func (p *store) getRecord(collection string, rkey string, targetDID syntax.DID, callerDID syntax.DID) (json.RawMessage, error) {
	// Run permissions before returning to the user
	authz, err := p.permissions.HasPermission(callerDID.String(), collection, rkey)
	if err != nil {
		return nil, err
	}

	if !authz {
		return nil, ErrUnauthorized
	}

	record, err := p.repo.getRecord(string(targetDID), rkey)
	if err != nil {
		return nil, err
	}

	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	return raw, nil
}
