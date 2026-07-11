package sap

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	car "github.com/ipld/go-car"

	"github.com/habitat-network/habitat/internal/spacecommit"
)

// recoveredRecord is a single record read from a com.atproto.space.getRepo CAR.
type recoveredRecord struct {
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	Cid        cid.Cid
	Value      map[string]any
}

// recoveredRepo is the parsed content of a com.atproto.space.getRepo CAR: the
// repo's signed head commit and its current record set.
type recoveredRepo struct {
	Commit  spacecommit.SignedCommit
	Records []recoveredRecord
}

// parseRepoCAR reads a com.atproto.space.getRepo CAR, mirroring the host's
// SerializeRepoCAR: two roots in order — the signed commit, then the DRISL
// index (a map of "{collection}/{rkey}" → record CID-link) — followed by the
// record blocks. It returns the signed commit and the decoded records.
func parseRepoCAR(r io.Reader) (recoveredRepo, error) {
	cr, err := car.NewCarReader(r)
	if err != nil {
		return recoveredRepo{}, fmt.Errorf("read car header: %w", err)
	}
	if len(cr.Header.Roots) != 2 {
		return recoveredRepo{}, fmt.Errorf("expected 2 car roots, got %d", len(cr.Header.Roots))
	}
	commitCID := cr.Header.Roots[0]
	indexCID := cr.Header.Roots[1]

	blocks := make(map[cid.Cid][]byte)
	for {
		blk, err := cr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return recoveredRepo{}, fmt.Errorf("read car block: %w", err)
		}
		blocks[blk.Cid()] = blk.RawData()
	}

	commitBytes, ok := blocks[commitCID]
	if !ok {
		return recoveredRepo{}, fmt.Errorf("commit block %s missing from car", commitCID)
	}
	commit, err := decodeSignedCommit(commitBytes)
	if err != nil {
		return recoveredRepo{}, err
	}

	indexBytes, ok := blocks[indexCID]
	if !ok {
		return recoveredRepo{}, fmt.Errorf("index block %s missing from car", indexCID)
	}
	index, err := atdata.UnmarshalCBOR(indexBytes)
	if err != nil {
		return recoveredRepo{}, fmt.Errorf("decode repo index: %w", err)
	}

	records := make([]recoveredRecord, 0, len(index))
	for path, link := range index {
		cl, ok := link.(atdata.CIDLink)
		if !ok {
			return recoveredRepo{}, fmt.Errorf("index entry %q is not a cid-link", path)
		}
		recCID := cl.CID()
		collection, rkey, err := splitRecordPath(path)
		if err != nil {
			return recoveredRepo{}, err
		}
		recBytes, ok := blocks[recCID]
		if !ok {
			return recoveredRepo{}, fmt.Errorf("record block %s for %q missing from car", recCID, path)
		}
		value, err := atdata.UnmarshalCBOR(recBytes)
		if err != nil {
			return recoveredRepo{}, fmt.Errorf("decode record %q: %w", path, err)
		}
		records = append(records, recoveredRecord{
			Collection: collection,
			Rkey:       rkey,
			Cid:        recCID,
			Value:      value,
		})
	}
	return recoveredRepo{Commit: commit, Records: records}, nil
}

// splitRecordPath splits a "{collection}/{rkey}" repo index path. Neither an
// NSID collection nor a record key contains a slash, so the first separates them.
func splitRecordPath(path string) (syntax.NSID, syntax.RecordKey, error) {
	collStr, rkeyStr, ok := strings.Cut(path, "/")
	if !ok {
		return "", "", fmt.Errorf("invalid record path %q", path)
	}
	collection, err := syntax.ParseNSID(collStr)
	if err != nil {
		return "", "", fmt.Errorf("parse collection in path %q: %w", path, err)
	}
	rkey, err := syntax.ParseRecordKey(rkeyStr)
	if err != nil {
		return "", "", fmt.Errorf("parse rkey in path %q: %w", path, err)
	}
	return collection, rkey, nil
}

// decodeSignedCommit decodes the CAR's signed-commit block, the DAG-CBOR
// encoding of network.habitat.space.defs#signedCommit.
func decodeSignedCommit(b []byte) (spacecommit.SignedCommit, error) {
	m, err := atdata.UnmarshalCBOR(b)
	if err != nil {
		return spacecommit.SignedCommit{}, fmt.Errorf("decode signed commit: %w", err)
	}
	ver, ok := m["ver"].(int64)
	if !ok {
		return spacecommit.SignedCommit{}, fmt.Errorf("signed commit ver is not an integer")
	}
	rev, ok := m["rev"].(string)
	if !ok {
		return spacecommit.SignedCommit{}, fmt.Errorf("signed commit rev is not a string")
	}
	hash, err := commitBytesField(m, "hash")
	if err != nil {
		return spacecommit.SignedCommit{}, err
	}
	if len(hash) == 0 {
		return spacecommit.SignedCommit{}, fmt.Errorf("signed commit missing hash")
	}
	// ikm/mac/sig are only needed for signature verification (a follow-up); a
	// commit that omits them is still usable for hash verification.
	ikm, err := commitBytesField(m, "ikm")
	if err != nil {
		return spacecommit.SignedCommit{}, err
	}
	mac, err := commitBytesField(m, "mac")
	if err != nil {
		return spacecommit.SignedCommit{}, err
	}
	sig, err := commitBytesField(m, "sig")
	if err != nil {
		return spacecommit.SignedCommit{}, err
	}
	return spacecommit.SignedCommit{
		Ver:  int(ver),
		Hash: hash,
		Ikm:  ikm,
		Mac:  mac,
		Sig:  sig,
		Rev:  rev,
	}, nil
}

// commitBytesField reads a lexicon bytes field from a decoded commit. A missing
// field decodes to nil; a present field must be a bytes value.
func commitBytesField(m map[string]any, key string) ([]byte, error) {
	switch v := m[key].(type) {
	case nil:
		return nil, nil
	case atdata.Bytes:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("signed commit %q is not a bytes field", key)
	}
}
