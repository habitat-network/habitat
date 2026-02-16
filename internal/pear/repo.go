package pear

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	_ "github.com/mattn/go-sqlite3"
)

// Persist private data within repos that mirror public repos.
// A repo currently implements four basic methods: putRecord, getRecord, uploadBlob, getBlob
// In the future, it is possible to implement sync endpoints and other methods.

// A sqlite-backed repo per user contains the following two columns:
// [did, record key, record value]
// For now, store all records in the same database. Eventually, this should be broken up into
// per-user databases or per-user MST repos.

// We really shouldn't have unexported types that get passed around outside the package, like to `main.go`
// Leaving this as-is for now.
type repo struct {
	db *gorm.DB
}

type Record struct {
	Did        string `gorm:"primaryKey"`
	Collection string `gorm:"primaryKey"`
	Rkey       string `gorm:"primaryKey"`
	Value      string
}

type Blob struct {
	gorm.Model
	Did      string
	Cid      string
	MimeType string
	Blob     []byte
}

// TODO: create table etc.
func NewRepo(db *gorm.DB) (*repo, error) {
	if err := db.AutoMigrate(&Record{}, &Blob{}); err != nil {
		return nil, err
	}
	return &repo{
		db: db,
	}, nil
}

// putRecord puts a record for the given rkey into the repo no matter what; if a record always exists, it is overwritten.
func (r *repo) putRecord(did string, collection string, rkey string, rec map[string]any, validate *bool) error {
	if validate != nil && *validate {
		err := atdata.Validate(rec)
		if err != nil {
			return err
		}
	}

	bytes, err := json.Marshal(rec)
	if err != nil {
		return err
	}

	// Store rkey directly (no concatenation with collection)
	record := Record{Did: did, Rkey: rkey, Collection: collection, Value: string(bytes)}
	// Always put (even if something exists).
	return gorm.G[Record](
		r.db,
		clause.OnConflict{
			Columns: []clause.Column{
				{Name: "did"},
				{Name: "collection"},
				{Name: "rkey"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"value"}),
		},
	).Create(context.Background(), &record)
}

var (
	ErrRecordNotFound       = fmt.Errorf("record not found")
	ErrMultipleRecordsFound = fmt.Errorf("multiple records found for desired query")
)

func (r *repo) getRecord(did string, collection string, rkey string) (*Record, error) {
	// Query using separate collection and rkey fields
	row, err := gorm.G[Record](
		r.db,
	).Where("did = ? AND collection = ? AND rkey = ?", did, collection, rkey).
		First(context.Background())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRecordNotFound
	} else if err != nil {
		return nil, err
	}
	return &row, nil
}

type blob struct {
	Ref      atdata.CIDLink `json:"cid"`
	MimeType string         `json:"mimetype"`
	Size     int64          `json:"size"`
}

func (r *repo) uploadBlob(did string, data []byte, mimeType string) (*blob, error) {
	// "blessed" CID type: https://atproto.com/specs/blob#blob-metadata
	cid, err := cid.NewPrefixV1(cid.Raw, multihash.SHA2_256).Sum(data)
	if err != nil {
		return nil, err
	}

	err = gorm.G[Blob](
		r.db,
		clause.OnConflict{UpdateAll: true},
	).Create(context.Background(), &Blob{
		Did:      did,
		Cid:      cid.String(),
		MimeType: mimeType,
		Blob:     data,
	})
	if err != nil {
		return nil, err
	}

	return &blob{
		Ref:      atdata.CIDLink(cid),
		MimeType: mimeType,
		Size:     int64(len(data)),
	}, nil
}

// getBlob gets a blob. this is never exposed to the server, because blobs can only be resolved via records that link them (see LexLink)
// besides exceptional cases like data migration which we do not support right now.
func (r *repo) getBlob(
	did string,
	cid string,
) (string /* mimetype */, []byte /* raw blob */, error) {
	row, err := gorm.G[Blob](
		r.db,
	).Where("did = ? and cid = ?", did, cid).First(context.Background())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil, ErrRecordNotFound
	} else if err != nil {
		return "", nil, err
	}

	return row.MimeType, row.Blob, nil
}

// listRecords implements repo.
func (r *repo) listRecords(
	did string,
	collection string,
	allow []string,
	deny []string,
) ([]Record, error) {
	if len(allow) == 0 {
		return []Record{}, nil
	}

	// Start with base query filtering by did and collection
	query := gorm.G[Record](
		r.db.Debug(),
	).Where("did = ?", did).
		Where("collection = ?", collection)

	// Build OR conditions for allow list
	// Permissions are stored in format: "collection" or "collection.*" or "collection.rkey"
	if len(allow) > 0 {
		// Check if any allow condition grants access to all records in the collection
		hasWildcard := false
		specificRkeys := []string{}

		for _, a := range allow {
			if strings.HasSuffix(a, "*") {
				// Wildcard match: "collection.*" or "parent.*" - check if it matches this collection
				prefix := strings.TrimSuffix(a, "*")
				// Trim trailing dot if present (e.g., "collection.*" -> "collection.")
				prefix = strings.TrimSuffix(prefix, ".")
				if prefix == collection || strings.HasPrefix(collection, prefix+".") {
					// Exact match or parent collection wildcard
					hasWildcard = true
					break // If we have a wildcard, we don't need to check specific rkeys
				}
			} else if a == collection {
				// Exact collection match: "collection" - match all rkeys in this collection
				hasWildcard = true
				break
			} else if strings.HasPrefix(a, collection+".") {
				// Specific record: "collection.rkey" - extract rkey
				rkey := strings.TrimPrefix(a, collection+".")
				specificRkeys = append(specificRkeys, rkey)
			} else {
				// Fallback: exact match on rkey (for backwards compatibility)
				specificRkeys = append(specificRkeys, a)
			}
		}

		// If we have a wildcard, no need to filter by rkey (collection filter is enough)
		// Otherwise, filter by specific rkeys
		if !hasWildcard && len(specificRkeys) > 0 {
			query = query.Where("rkey IN ?", specificRkeys)
		}
		// If hasWildcard is true, we don't add any rkey filter (matches all in collection)
	}

	// Build deny conditions
	// Permissions are stored in format: "collection" or "collection.*" or "collection.rkey"
	hasCollectionDeny := false
	deniedRkeys := []string{}

	for _, d := range deny {
		if strings.HasSuffix(d, "*") {
			// Wildcard deny: "collection.*" or "parent.*" - check if it matches this collection
			prefix := strings.TrimSuffix(d, "*")
			// Trim trailing dot if present (e.g., "collection.*" -> "collection.")
			prefix = strings.TrimSuffix(prefix, ".")
			// Check exact match first
			if prefix == collection {
				hasCollectionDeny = true
				break
			}
			// Check if this is a parent collection wildcard (e.g., "network.habitat.*" matches "network.habitat.collection-2")
			if strings.HasPrefix(collection, prefix+".") {
				hasCollectionDeny = true
				break
			}
		} else if d == collection {
			// Exact collection deny: "collection" - deny all rkeys in this collection
			hasCollectionDeny = true
			break
		} else if strings.HasPrefix(d, collection+".") {
			// Specific record deny: "collection.rkey" - extract rkey and deny
			rkey := strings.TrimPrefix(d, collection+".")
			deniedRkeys = append(deniedRkeys, rkey)
		} else {
			// Fallback: exact deny on rkey (for backwards compatibility)
			deniedRkeys = append(deniedRkeys, d)
		}
	}

	// Apply deny conditions
	if hasCollectionDeny {
		// Deny all records in this collection - return empty result
		return []Record{}, nil
	} else if len(deniedRkeys) > 0 {
		// Deny specific rkeys
		query = query.Where("rkey NOT IN ?", deniedRkeys)
	}

	// Cursor-based pagination -- unimplemented
	/*
		if params.Cursor != "" {
			query = query.Where("rkey > ?", params.Cursor)
		}

		// Limit
		if params.Limit != 0 {
			query = query.Limit(int(params.Limit))
		}
	*/

	// Order by rkey for consistent pagination
	query = query.Order("rkey ASC")

	// Execute query
	rows, err := query.Find(context.Background())
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	return rows, nil
}

// listRecordsByOwners queries records from multiple owners for a given collection
func (r *repo) listRecordsByOwners(ownerDIDs []string, collection string) ([]Record, error) {
	if len(ownerDIDs) == 0 {
		return []Record{}, nil
	}

	query := gorm.G[Record](r.db).
		Where("did IN ?", ownerDIDs).
		Where("collection = ?", collection).
		Order("did ASC, rkey ASC")

	records, err := query.Find(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to query records by owners: %w", err)
	}
	return records, nil
}

// listSpecificRecords queries specific records by a list of (did, rkey) pairs for a given collection
// recordPairs is a slice of structs with Owner and Rkey fields
func (r *repo) listSpecificRecords(collection string, recordPairs []struct{ Owner, Rkey string }) ([]Record, error) {
	if len(recordPairs) == 0 {
		return []Record{}, nil
	}

	// Build OR conditions for (did, rkey) pairs
	// Format: (did = owner1 AND rkey = rkey1) OR (did = owner2 AND rkey = rkey2) OR ...
	query := gorm.G[Record](r.db).
		Where("collection = ?", collection)

	// Build OR conditions using GORM's Or method
	if len(recordPairs) > 0 {
		query = query.Where("(did = ? AND rkey = ?)", recordPairs[0].Owner, recordPairs[0].Rkey)
		for i := 1; i < len(recordPairs); i++ {
			query = query.Or("(did = ? AND rkey = ?)", recordPairs[i].Owner, recordPairs[i].Rkey)
		}
	}

	query = query.Order("did ASC, rkey ASC")

	records, err := query.Find(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to query specific records: %w", err)
	}
	return records, nil
}
