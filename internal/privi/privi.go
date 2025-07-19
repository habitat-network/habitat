package privi

import (
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/eagraf/habitat-new/internal/permissions"
)

type record map[string]any

// A record key is a string
type recordKey string

// Lexicon NSID -> records for that lexicon.
// A record is stored as raw bytes and keyed by its record key (rkey).
//
// TODO: the internal store should be an MST for portability / compatiblity with conventional atproto  methods.
type inMemoryRepo map[syntax.NSID]map[recordKey]record

// putRecord puts a record for the given rkey into the repo no matter what; if a record always exists, it is overwritten.
func (r inMemoryRepo) putRecord(collection string, rec record, rkey string, validate *bool) error {
	if validate != nil && *validate {
		err := data.Validate(rec)
		if err != nil {
			return err
		}
	}

	coll, ok := r[syntax.NSID(collection)]
	if !ok {
		coll = make(map[recordKey]record)
		r[syntax.NSID(collection)] = coll
	}

	// Always put (even if something exists).
	coll[recordKey(rkey)] = rec
	return nil
}

func (r inMemoryRepo) getRecord(collection string, rkey string) (record, bool) {
	coll, ok := r[syntax.NSID(collection)]
	if !ok {
		return nil, false
	}

	record, ok := coll[recordKey(rkey)]
	return record, ok
}

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

	// TODO: this should be a portable MST the same as stored in the PDS. For ease/demo purposes, just use an
	// in-memory store.
	repo inMemoryRepo
}

var (
	ErrPublicRecordExists      = fmt.Errorf("a public record exists with the same key")
	ErrNoPutsOnEncryptedRecord = fmt.Errorf("directly put-ting to this lexicon is not valid")
	ErrNotLocalRepo            = fmt.Errorf("the desired did does not live on this repo")
	ErrUnauthorized            = fmt.Errorf("unauthorized request")
	ErrRecordNotFound          = fmt.Errorf("record not found")
)

// TODO: take in a carfile/sqlite where user's did is persisted
func newStore(did syntax.DID, perms permissions.Store) *store {
	return &store{
		did:         did,
		permissions: perms,
		repo:        make(inMemoryRepo),
	}
}

// putRecord puts the given record on the repo connected to this store (currently an in-memory repo that is a KV store)
// It does not do any encryption, permissions, auth, etc. It is assumed that only the owner of the store can call this and that
// is gated by some higher up level. This should be re-written in the future to not give any incorrect impression.
func (p *store) putRecord(collection string, record map[string]any, rkey string, validate *bool) error {
	// It is assumed right now that if this endpoint is called, the caller wants to put a private record into privi.
	return p.repo.putRecord(collection, record, rkey, validate)
}

type GetRecordResponse struct {
	Cid   *string `json:"cid"`
	Uri   string  `json:"uri"`
	Value any     `json:"value"`
}

// getRecord checks permissions on callerDID and then passes through to `repo.getRecord`.
func (p *store) getRecord(collection string, rkey string, callerDID syntax.DID) (json.RawMessage, error) {
	// Run permissions before returning to the user
	authz, err := p.permissions.HasPermission(callerDID.String(), collection, rkey)
	if err != nil {
		return nil, err
	}

	if !authz {
		return nil, ErrUnauthorized
	}

	record, ok := p.repo.getRecord(collection, rkey)
	if !ok {
		// TODO: is this the right thing to return here?
		return nil, ErrRecordNotFound
	}

	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	return raw, nil
}
