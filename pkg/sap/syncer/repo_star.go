package syncer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"

	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/star"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// recoveredRecord is a single record read from a getRepo STAR archive.
type recoveredRecord struct {
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	Cid        cid.Cid
	Value      map[string]any
}

// recoveredRepo is the parsed content of a getRepo STAR archive: the repo's
// head commit and its current record set.
type recoveredRepo struct {
	Commit  spacecommit.Commit
	Records []recoveredRecord
}

const (
	// repoStarType is the STAR profile of a getRepo archive.
	repoStarType = "atproto-space-repo"
	// maxRecordSize caps a record's DRISL encoding, as the profile does.
	maxRecordSize = 1024 * 1024
)

// parseRepoStar reads a getRepo STAR archive, mirroring the host's
// SerializeRepoStar: the metadata is the repo's commit, typed by the
// atproto-space-repo profile, followed by one "{collection}/{rkey}" → DRISL
// record pair per record in strict bytewise order of the path. space and repo
// are what was requested; the commit must name them.
func parseRepoStar(
	r io.Reader,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
) (recoveredRepo, error) {
	sr, err := star.NewReader(r, maxRecordSize)
	if err != nil {
		return recoveredRepo{}, fmt.Errorf("read star header: %w", err)
	}
	if t := sr.Meta["$type"]; t != repoStarType {
		return recoveredRepo{}, fmt.Errorf("unexpected star profile %v", t)
	}
	if got, _ := sr.Meta["repo"].(string); got != repo.String() {
		return recoveredRepo{}, fmt.Errorf("commit is for repo %q, want %q", got, repo)
	}
	if got, _ := sr.Meta["space"].(string); got != space.String() {
		return recoveredRepo{}, fmt.Errorf("commit is for space %q, want %q", got, space)
	}
	commit, err := decodeCommit(sr.Meta)
	if err != nil {
		return recoveredRepo{}, err
	}

	var records []recoveredRecord
	var prev []byte
	for {
		key, value, err := sr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return recoveredRepo{}, fmt.Errorf("read star pair: %w", err)
		}
		path := string(key)
		if prev != nil && bytes.Compare(key, prev) <= 0 {
			return recoveredRepo{}, fmt.Errorf("records out of order at %q", path)
		}
		prev = key
		collection, rkey, err := splitRecordPath(path)
		if err != nil {
			return recoveredRepo{}, err
		}
		recCID, err := cid.NewPrefixV1(cid.DagCBOR, multihash.SHA2_256).Sum(value)
		if err != nil {
			return recoveredRepo{}, fmt.Errorf("compute cid of %q: %w", path, err)
		}
		decoded, err := atdata.UnmarshalCBOR(value)
		if err != nil {
			return recoveredRepo{}, fmt.Errorf("decode record %q: %w", path, err)
		}
		records = append(records, recoveredRecord{
			Collection: collection,
			Rkey:       rkey,
			Cid:        recCID,
			Value:      decoded,
		})
	}
	return recoveredRepo{Commit: commit, Records: records}, nil
}

// splitRecordPath splits a "{collection}/{rkey}" record path. Neither an
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

// decodeCommit decodes the commit carried as a getRepo archive's metadata:
// the profile's ver, repo, space, rev and data (the commit's set hash). Its MAC
// is in the response's Atproto-Space-Mac header, not the metadata.
func decodeCommit(m map[string]any) (spacecommit.Commit, error) {
	ver, ok := m["ver"].(int64)
	if !ok {
		return spacecommit.Commit{}, fmt.Errorf("commit ver is not an integer")
	}
	rev, ok := m["rev"].(string)
	if !ok {
		return spacecommit.Commit{}, fmt.Errorf("commit rev is not a string")
	}
	repo, _ := m["repo"].(string)
	space, _ := m["space"].(string)
	data, err := commitBytesField(m, "data")
	if err != nil {
		return spacecommit.Commit{}, err
	}
	if len(data) == 0 {
		return spacecommit.Commit{}, fmt.Errorf("commit missing data")
	}
	return spacecommit.Commit{
		Ver:   int(ver),
		Repo:  syntax.DID(repo),
		Space: habitat_syntax.SpaceURI(space),
		Rev:   rev,
		Data:  data,
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
		return nil, fmt.Errorf("commit %q is not a bytes field", key)
	}
}
