package privy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/api/agnostic"
	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// Privy is an ATProto PDS Wrapper which allows for storing & getting private data.
// It does this by encrypting data, then storing it in blob. A special lexicon for this purpose,
// identified by com.habitat.encryptedRecord, points to the blob storing the actual data.
//
// This encryption layer is transparent to the caller -- Privy.PutRecord() and Privy.GetRecord() have
// the same API has com.atproto.putRecord and com.atproto.getRecord.
//
// TODO: formally define the com.habitat.encryptedRecord and change it to a domain we actually own :)
type store struct {
	e Encrypter
}

const encryptedRecordNSID = "com.habitat.encryptedRecord"

func encryptedRecordRKey(collection string, rkey string) string {
	return fmt.Sprintf("enc:%s:%s", collection, rkey)
}

type encryptedRecord struct {
	Data util.BlobSchema `json:"data" cborgen:"data"`
}

var (
	ErrPublicRecordExists               = fmt.Errorf("a public record exists with the same key")
	ErrNoPutsOnEncryptedRecord          = fmt.Errorf("directly put-ting to this lexicon is not valid")
	ErrNoEncryptedGetsOnEncryptedRecord = fmt.Errorf("calling getEncryptedRecord on a %s is not supported", encryptedRecordNSID)
	ErrEncryptedRecordNilValue          = fmt.Errorf("a %s record was found but it has a nil value", encryptedRecordNSID)
)

// Returns true if err indicates the RecordNotFound error
func errorIsNoRecordFound(err error) bool {
	// TODO: Not sure if the atproto lib has an error to directly use with errors.Is()
	return strings.Contains(err.Error(), "RecordNotFound") || strings.Contains(err.Error(), "Could not locate record")
}

// type encryptedRecord map[string]any
// the shape of the lexicon is { "cid": <cid pointing to the encrypted blob> }

// putRecord with encryption wrapper around this
// ONLY YOU CAN CALL PUT RECORD, NO ONE ELSE
// Our security relies on this -- if this wasn't true then theoretically an attacker could call putRecord trying different rkey.
// If they were unable to create with an rkey, that means that it exists privately.
func (p *store) putRecord(ctx context.Context, xrpc *xrpc.Client, input *agnostic.RepoPutRecord_Input, encrypt bool) (*agnostic.RepoPutRecord_Output, error) {
	if input.Collection == encryptedRecordNSID {
		return nil, ErrNoPutsOnEncryptedRecord
	}

	// Not encrypted -- blindly forward the request to PDS
	if !encrypt {
		return agnostic.RepoPutRecord(ctx, xrpc, input)
	}

	// Check if a record under this collection already exists publicly with this rkey
	// if so, return error (need a different rkey)
	_, err := agnostic.RepoGetRecord(ctx, xrpc, "", input.Collection, input.Repo, input.Rkey)
	if err == nil {
		return nil, fmt.Errorf("%w: %s", ErrPublicRecordExists, input.Rkey)
	} else if !errorIsNoRecordFound(err) {
		return nil, err
	}

	// If we're here, then this record is to be encrypted and there is no existing public record with rkey
	// Encrypted -- unpack the request and use special habitat encrypted record lexicon
	return p.putEncryptedRecord(ctx, xrpc, input.Collection, input.Repo, input.Record, input.Rkey, input.Validate)
}

type GetRecordResponse struct {
	Cid   *string `json:"cid"`
	Uri   string  `json:"uri"`
	Value any     `json:"value"`
}

// There are some different scenarios here: (TODO: write tests for all of these scenarios)
//
//	1a) cid = that of a non-com.habitat.encryptedRecord --> return that data as-is.
//	1b) cid = that of a com.habitat.encryptedRecord --> return that data as-is, it will simply be encrypted. getRecord will not attempt to authz and decrypt.
//	1c) cid = that of a private or public blob --> return that blob as-is.
//
// If no cid is provided, fallback to using collection + rkey as the lookup:
//
//	2a) collection + rkey = a com.habitat.encryptedRecord --> return that data as-is if exists, which contains a cid pointer to a blob. if no such record exists, return
//	--) collection + rkey = a non-com.habitat.encryptedRecord:
//	   2b) if a corresponding record is found, return that
//	   2c) if no corresponding record is found, attempt to decrypt the record a com.habitat.encryptedRecord would point to for that collection + rkey
//
// Keeping this API the same as com.atproto.getRecord
func (p *store) getRecord(ctx context.Context, xrpc *xrpc.Client, cid string, collection string, did string, rkey string) (*agnostic.RepoGetRecord_Output, error) {
	// Attempt to get a public record corresponding to the Collection + Repo + Rkey.
	// If the given cid does not point to anything, the GetRecord endpoint returns an error.
	// Record not found results in an error, as does any other non-200 response from the endpoint.
	// Cases 1a - 1c are handled directly by this case.
	output, err := agnostic.RepoGetRecord(ctx, xrpc, cid, collection, did, rkey)
	// If this is a cid lookup (cases 1a-1c) or the record was found (2a + 2b), simply return ()
	if err == nil {
		return output, nil
	}

	// If the record with the given collection + rkey identifier was not found (case 2c), attempt to get a private record with permissions look up.
	if strings.Contains(err.Error(), "RecordNotFound") || strings.Contains(err.Error(), "Could not locate record") {
		return p.getEncryptedRecord(ctx, xrpc, cid, collection, did, rkey)
	}
	// Otherwise the lookup failed in some other way, return the error
	return nil, err
}

// getEncryptedRecord assumes that the record given by the cid + collection + rkey + did has been encrypted via putRecord and fetches it
// Keeping this API the same as com.atproto.getRecord
func (p *store) getEncryptedRecord(ctx context.Context, xrpc *xrpc.Client, cid string, collection string, did string, rkey string) (*agnostic.RepoGetRecord_Output, error) {
	if collection == encryptedRecordNSID {
		return nil, ErrNoEncryptedGetsOnEncryptedRecord
	}
	encKey := encryptedRecordRKey(collection, rkey)
	output, err := agnostic.RepoGetRecord(ctx, xrpc, cid, encryptedRecordNSID, did, encKey)
	if err != nil {
		return nil, err
	}

	// TODO: I'm not sure if this error makes sense or how this might happen
	if output.Value == nil {
		return nil, ErrEncryptedRecordNilValue
	}
	bytes, err := output.Value.MarshalJSON()
	if err != nil {
		return nil, err
	}

	// Run permissions before returning to the user
	// if HasAccess(did, collection, rkey) { .... }
	var record encryptedRecord
	err = json.Unmarshal(bytes, &record)
	// Unfortunate that we need to MarshalJSON to turn it back into bytes -- the RepoGetRecord function probably Unmarshals :/
	if err != nil {
		return nil, err
	}

	// blob contains the encrypted lexicon written by the user
	blob, err := atproto.SyncGetBlob(ctx, xrpc, record.Data.Ref.String(), did)
	if err != nil {
		return nil, err
	}

	dec, err := p.e.Decrypt(rkey, blob)
	if err != nil {
		return nil, err
	}

	asJson := json.RawMessage(dec)
	return &agnostic.RepoGetRecord_Output{
		Cid:   output.Cid,
		Uri:   output.Uri,
		Value: &asJson,
	}, nil
}

// puttEncryptedRecord encrypts the given record.
func (p *store) putEncryptedRecord(ctx context.Context, xrpc *xrpc.Client, collection string, repo string, record map[string]any, rkey string, validate *bool) (*agnostic.RepoPutRecord_Output, error) {
	if collection == encryptedRecordNSID {
		return nil, ErrNoPutsOnEncryptedRecord
	}
	// If we're here, then this record is to be encrypted and there is no existing public record with rkey
	// Encrypted -- unpack the request and use special habitat encrypted record lexicon
	marshalled, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}

	enc, err := p.e.Encrypt(rkey, marshalled)
	if err != nil {
		return nil, err
	}

	if validate != nil && *validate {
		err = data.Validate(record)
		if err != nil {
			return nil, err
		}
	}

	blobOut, err := atproto.RepoUploadBlob(ctx, xrpc, bytes.NewBuffer(enc))
	if err != nil {
		return nil, err
	}

	// CID is returned on uploadBlob
	blob := blobOut.Blob
	encKey := encryptedRecordRKey(collection, rkey)
	// It's our fault if this fails, but always attempt to validate the habitat encoded request
	validateEnc := false
	// TODO: let's make a helper function for this
	encInput := &agnostic.RepoPutRecord_Input{
		Collection: encryptedRecordNSID,
		Repo:       repo,
		Rkey:       encKey,
		Validate:   &validateEnc,
		Record: map[string]any{
			"data": blob,
		},
	}
	return agnostic.RepoPutRecord(ctx, xrpc, encInput)
}
