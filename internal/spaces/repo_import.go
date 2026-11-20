package spaces

import (
	"bytes"
	"context"
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

// ErrInvalidRepoArchive is returned by [ReadRepoStar] for a STAR archive that
// is malformed or fails verification. importRepo answers it as InvalidRepoFile.
var ErrInvalidRepoArchive = errors.New("invalid repo archive")

// ErrReservedCollection is returned by [ImportRepo] for an archive holding
// records in a collection that is managed through its own endpoints (see
// [habitat_syntax.ReservedCollections]).
var ErrReservedCollection = errors.New("archive holds records in a reserved collection")

// maxImportRecordSize caps a record's DRISL encoding, as the STAR profile does.
const maxImportRecordSize = 1024 * 1024

// ImportedRecord is a record read from a repo archive, in the canonical
// encoding every record is stored in.
type ImportedRecord struct {
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	Cid        cid.Cid
	Value      MarshaledRecord
}

// RepoArchive is a verified com.atproto.space.getRepo STAR archive: the repo's
// commit and its records.
type RepoArchive struct {
	Commit  spacecommit.Commit
	Records []ImportedRecord
}

// ReadRepoStar reads and verifies a repo archive, as importRepo takes it.
//
// Unlike a syncer's getRepo response, an imported archive comes with no MAC to
// authenticate its commit: the caller is importing their own repo, so the
// commit is only checked against the records. Each record must be canonical
// DRISL, the paths must be in strict bytewise order, and the records' set hash
// must be the commit's data.
func ReadRepoStar(r io.Reader) (*RepoArchive, error) {
	sr, err := star.NewReader(r, maxImportRecordSize)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRepoArchive, err)
	}
	commit, err := commitFromStarMeta(sr.Meta)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRepoArchive, err)
	}

	var (
		records []ImportedRecord
		prev    []byte
		hash    spacecommit.LtHash
	)
	for {
		key, value, err := sr.Next()
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidRepoArchive, err)
		}
		if prev != nil && bytes.Compare(key, prev) <= 0 {
			return nil, fmt.Errorf("%w: records out of order at %q", ErrInvalidRepoArchive, key)
		}
		prev = key
		rec, err := importedRecord(string(key), value)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidRepoArchive, err)
		}
		hash.Add(spacecommit.RecordElement(rec.Collection, rec.Rkey, rec.Cid.String()))
		records = append(records, rec)
	}
	if !commit.Matches(&hash) {
		return nil, fmt.Errorf("%w: records do not match the commit", ErrInvalidRepoArchive)
	}
	return &RepoArchive{Commit: commit, Records: records}, nil
}

// commitFromStarMeta reads the commit carried as an archive's metadata.
func commitFromStarMeta(meta map[string]any) (spacecommit.Commit, error) {
	if t := meta["$type"]; t != RepoStarType {
		return spacecommit.Commit{}, fmt.Errorf("unexpected star profile %v", t)
	}
	ver, ok := meta["ver"].(int64)
	if !ok || ver != spacecommit.Version {
		return spacecommit.Commit{}, fmt.Errorf("unsupported commit version %v", meta["ver"])
	}
	repo, err := syntax.ParseDID(stringField(meta, "repo"))
	if err != nil {
		return spacecommit.Commit{}, fmt.Errorf("commit repo: %w", err)
	}
	space, err := habitat_syntax.ParseSpaceURI(stringField(meta, "space"))
	if err != nil {
		return spacecommit.Commit{}, fmt.Errorf("commit space: %w", err)
	}
	rev, err := syntax.ParseTID(stringField(meta, "rev"))
	if err != nil {
		return spacecommit.Commit{}, fmt.Errorf("commit rev: %w", err)
	}
	data, ok := meta["data"].(atdata.Bytes)
	if !ok || len(data) == 0 {
		return spacecommit.Commit{}, errors.New("commit data is missing")
	}
	return spacecommit.Commit{
		Ver: int(ver), Repo: repo, Space: space, Rev: rev.String(), Data: data,
	}, nil
}

func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// importedRecord parses one "{collection}/{rkey}" → DRISL record pair.
func importedRecord(path string, value []byte) (ImportedRecord, error) {
	collStr, rkeyStr, ok := strings.Cut(path, "/")
	if !ok {
		return ImportedRecord{}, fmt.Errorf("invalid record path %q", path)
	}
	collection, err := syntax.ParseNSID(collStr)
	if err != nil {
		return ImportedRecord{}, fmt.Errorf("collection in path %q: %w", path, err)
	}
	rkey, err := syntax.ParseRecordKey(rkeyStr)
	if err != nil {
		return ImportedRecord{}, fmt.Errorf("rkey in path %q: %w", path, err)
	}
	decoded, err := atdata.UnmarshalCBOR(value)
	if err != nil {
		return ImportedRecord{}, fmt.Errorf("record %q: %w", path, err)
	}
	// The decoder accepts some non-canonical forms. A record that was not
	// canonical DRISL would no longer match its CID once stored re-encoded.
	canonical, err := atdata.MarshalCBOR(decoded)
	if err != nil || !bytes.Equal(canonical, value) {
		return ImportedRecord{}, fmt.Errorf("record %q is not canonical DRISL", path)
	}
	c, err := cid.NewPrefixV1(cid.DagCBOR, multihash.SHA2_256).Sum(value)
	if err != nil {
		return ImportedRecord{}, fmt.Errorf("cid of record %q: %w", path, err)
	}
	return ImportedRecord{
		Collection: collection, Rkey: rkey, Cid: c, Value: MarshaledRecord(value),
	}, nil
}

// ClearRepo deletes every record repo holds in space, as
// com.atproto.space.clearRecords does, as one batch. Records in reserved
// collections are kept: they are the space's configuration, managed through
// their own endpoints, not the account's data. It does nothing if there is
// nothing to delete.
func ClearRepo(
	ctx context.Context,
	s Store,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
) error {
	existing, err := s.ListRecords(ctx, space, repo, nil)
	if err != nil {
		return fmt.Errorf("list records: %w", err)
	}
	var writes []Write
	for _, rec := range existing {
		if habitat_syntax.ReservedCollections.Contains(rec.Collection) {
			continue
		}
		writes = append(writes, Write{
			Action: WriteDelete, Collection: rec.Collection, Rkey: rec.Rkey,
		})
	}
	if len(writes) == 0 {
		return nil
	}
	if _, err := s.ApplyWrites(ctx, space, repo, writes); err != nil {
		return fmt.Errorf("clear records: %w", err)
	}
	return nil
}

// ImportRepo makes repo's records in space exactly those of a verified
// archive, as com.atproto.space.importRepo does: records the archive lacks are
// deleted, the rest are created or updated, in one batch (so one new
// revision). The space must exist on this host. Records in reserved
// collections are left alone, and an archive holding any is refused with
// [ErrReservedCollection]. Blobs the records reference are uploaded
// afterwards; they are associated here without being checked.
func ImportRepo(
	ctx context.Context,
	s Store,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	records []ImportedRecord,
) error {
	type path struct {
		collection syntax.NSID
		rkey       syntax.RecordKey
	}
	incoming := make(map[path]struct{}, len(records))
	for _, rec := range records {
		if habitat_syntax.ReservedCollections.Contains(rec.Collection) {
			return fmt.Errorf("%w: %s", ErrReservedCollection, rec.Collection)
		}
		incoming[path{rec.Collection, rec.Rkey}] = struct{}{}
	}

	existing, err := s.ListRecords(ctx, space, repo, nil)
	if err != nil {
		return fmt.Errorf("list records: %w", err)
	}
	held := make(map[path]struct{}, len(existing))
	var writes []Write
	for _, rec := range existing {
		p := path{rec.Collection, rec.Rkey}
		if habitat_syntax.ReservedCollections.Contains(rec.Collection) {
			continue
		}
		held[p] = struct{}{}
		if _, keep := incoming[p]; !keep {
			writes = append(writes, Write{
				Action: WriteDelete, Collection: rec.Collection, Rkey: rec.Rkey,
			})
		}
	}
	for _, rec := range records {
		action := WriteCreate
		if _, ok := held[path{rec.Collection, rec.Rkey}]; ok {
			action = WriteUpdate
		}
		writes = append(writes, Write{
			Action: action, Collection: rec.Collection, Rkey: rec.Rkey, Value: rec.Value,
		})
	}
	if len(writes) == 0 {
		// Still say whether the space exists, so importing into a missing
		// space is never silently accepted.
		ok, err := s.CheckSpaceExists(ctx, space)
		if err != nil {
			return fmt.Errorf("check space: %w", err)
		} else if !ok {
			return ErrSpaceNotFound
		}
		return nil
	}
	if _, err := s.ApplyWrites(ctx, space, repo, writes); err != nil {
		return fmt.Errorf("import repo: %w", err)
	}
	return nil
}
