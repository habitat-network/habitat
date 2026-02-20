package permissions

import (
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xmaps"
	"github.com/bradenaw/juniper/xslices"
	"gorm.io/gorm"
)

type Store interface {
	// Whether the requester is directly granted permission to this record.
	// If the requester has indirect permissions via cliques, this returns false.
	HasDirectPermission(
		requester syntax.DID,
		owner syntax.DID,
		collection syntax.NSID,
		rkey syntax.RecordKey,
	) (bool, error)
	AddPermissions(
		grantees []Grantee,
		owner syntax.DID,
		collection syntax.NSID,
		rkey syntax.RecordKey,
	) error
	RemovePermissions(
		grantee []Grantee,
		owner syntax.DID,
		collection syntax.NSID,
		rkey syntax.RecordKey,
	) error
	ListPermissions(
		grantee syntax.DID,
		owner syntax.DID,
		collection syntax.NSID,
		rkey syntax.RecordKey,
	) ([]Permission, error)
}

// RecordPermission represents a specific record permission (owner + rkey)
type RecordPermission struct {
	Owner string
	Rkey  string
}

type store struct {
	// Backing data store for some subset of data that this enforcer
	db *gorm.DB
}

var _ Store = (*store)(nil)

type Effect string

const (
	Allow Effect = "allow"
	Deny  Effect = "deny"
)

// Public — exported for use elsewhere, typed with Grantee interface
type Permission struct {
	Grantee    Grantee
	Owner      syntax.DID
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	Effect     Effect
}

// Permission represents a permission entry in the database. this type defines the Gorm DB model of permissions, not what is exported to other packages.
type permission struct {
	Grantee    string `gorm:"primaryKey"`
	Owner      string `gorm:"primaryKey"`
	Collection string `gorm:"primaryKey"`
	Rkey       string `gorm:"primaryKey"`
	Effect     string `gorm:"not null;check:effect IN ('allow', 'deny')"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NewStore creates a new db-backed permission store.
// The store manages permissions at different granularities:
// - Whole NSID prefixes: "network.habitat.*"
// - Specific NSIDs: "network.habitat.collection"
// - Specific records: "network.habitat.collection.recordKey"
func NewStore(db *gorm.DB) (*store, error) {
	// AutoMigrate will create the table with all indexes defined in the Permission struct
	err := db.AutoMigrate(&permission{})
	if err != nil {
		return nil, fmt.Errorf("failed to migrate permissions table: %w", err)
	}

	return &store{db: db}, nil
}

// HasPermission checks if a requester has permission to access a specific record.
func (s *store) HasDirectPermission(
	requester syntax.DID,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) (bool, error) {
	// Owner always has permission
	if requester == owner {
		return true, nil
	}
	var permission permission
	err := s.db.Where("grantee = ?", requester.String()).
		Where("owner = ?", owner.String()).
		Where("collection = ?", collection.String()).
		// permissions with empty rkeys grant the entire collection
		Where("rkey = ? OR rkey = ''", rkey.String()).
		// prioritize more specific permission and denies
		Order("LENGTH(rkey) DESC, effect DESC").
		Limit(1).
		First(&permission).
		Error
	if err == gorm.ErrRecordNotFound {
		// No permission found, deny by default
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("failed to query permission: %w", err)
	}

	return permission.Effect == string(Allow), nil
}

// AddPermissions grants read permission for an entire collection or specific record.
// If rkey is empty, it grants read permission for the whole collection.
// Will delete redundant permissions that are covered by the new grant.
// Will not add redundant permssions that are less powerful than existing ones
// Will not error in cases where no work is done
func (s *store) AddPermissions(
	granteesTyped []Grantee,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) error {
	if collection == "" {
		return fmt.Errorf("collection is required")
	}

	grantees := xslices.Map(granteesTyped, func(g Grantee) string {
		return g.String()
	})

	var existingCollectionPermissions []permission
	if rkey == "" { /* collection-level permission */
		// delete redundant allow permissions that are less powerful
		result := s.db.Where("grantee IN ?", grantees).
			Where("owner = ?", owner).
			Where("rkey != ''").
			Where("effect = 'allow'").
			Delete(&permission{})
		if result.Error != nil {
			return fmt.Errorf("failed to remove redundant allow permissions: %w", result.Error)
		}
	} else { /* record-level permission */
		// check if there are existing grantess with collection-level permissions.
		// if so, we shouldn't add new ones for those grantees
		if err := s.db.Where("grantee IN ?", grantees).
			Where("owner = ?", owner).
			Where("collection = ?", collection).
			Where("rkey = ''").Find(&existingCollectionPermissions).Error; err != nil {
			return fmt.Errorf("failed to query existing collection permissions: %w", err)
		}
	}
	if len(existingCollectionPermissions) == len(grantees) {
		// all grantess already have collection-level permissions so don't do anything
		return nil
	}

	existingCollectionGrantees := xmaps.Set[string]{}
	for _, perm := range existingCollectionPermissions {
		existingCollectionGrantees.Add(perm.Grantee)
	}
	// only add permissions for those that don't already have access
	granteesSet := xmaps.Difference(xmaps.SetFromSlice(grantees), existingCollectionGrantees)
	permissions := []permission{}
	for grantee := range granteesSet {
		permissions = append(permissions, permission{
			Grantee:    grantee,
			Owner:      owner.String(),
			Effect:     string(Allow),
			Collection: collection.String(),
			Rkey:       rkey.String(),
		})
	}
	// Delete any existing permissions (allow or deny) for these grantees+record before inserting fresh allow permissions.
	// This is jank and its because SQLITE/postgres differ in the ON CONFLICT specs. We should fix this.
	if err := s.db.Where("grantee IN ?", grantees).
		Where("owner = ?", owner).
		Where("collection = ?", collection).
		Where("rkey = ?", rkey).
		Delete(&permission{}).Error; err != nil {
		return fmt.Errorf("failed to clear existing permissions before insert: %w", err)
	}
	if err := s.db.Create(&permissions).Error; err != nil {
		return fmt.Errorf("failed to add lexicon permission: %w", err)
	}
	return nil
}

// RemovePermissions removes read permission for an entire collection or specific record.
// If rkey is empty, it revokes read permission for the whole collection.
// If the user already has access to the collection, it will add a deny permission for rkey.
// Otherwise will delete the specific permission if it exists.
// Will not error if there are no permissions to remove.
func (s *store) RemovePermissions(
	granteesTyped []Grantee,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) error {
	fmt.Println("remove readpermission", granteesTyped, owner, collection, rkey)
	grantees := xslices.Map(granteesTyped, func(g Grantee) string {
		return g.String()
	})
	// If the removal is on an entire collection, delete all allow permissions for records in the collection or the entire collection.
	if rkey == "" {
		// delete all permissions for this collection
		result := s.db.Where("grantee IN ?", grantees).
			Where("owner = ?", owner).
			Where("collection = ?", collection).
			Delete(&permission{})
		if result.Error != nil {
			return fmt.Errorf("failed to remove collection permission: %w", result.Error)
		}
		return nil
	}

	fmt.Println("specific rkey")
	// Otherwise, the removal is on a specific record key, so add a deny effect on the specific record for a batch of grantees.
	recordPerms := make([]*permission, len(grantees))
	for i, grantee := range grantees {
		recordPerms[i] = &permission{
			Grantee:    grantee,
			Owner:      owner.String(),
			Collection: collection.String(),
			Rkey:       rkey.String(),
			Effect:     string(Deny),
		}
	}

	// Delete any existing permission for this record before inserting the deny.
	// This is jank and its because SQLITE/postgres differ in the ON CONFLICT specs. We should fix this.
	if err := s.db.Where("grantee IN ?", grantees).
		Where("owner = ?", owner).
		Where("collection = ?", collection).
		Where("rkey = ?", rkey).
		Delete(&permission{}).Error; err != nil {
		return fmt.Errorf("failed to clear existing permissions before deny insert: %w", err)
	}

	fmt.Println("creating deny")
	if err := s.db.Create(recordPerms).Error; err != nil {
		return fmt.Errorf("failed to add deny permissions: %w", err)
	}
	return nil
}

// ListPermissions returns the permissions available to this particular combination of inputs.
// Any "" inputs are not filtered by.
func (s *store) ListPermissions(
	grantee syntax.DID,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) ([]Permission, error) {
	// TODO prevent table scans if all arguments are ""
	var queried []permission
	query := s.db
	if owner.String() != "" {
		query = query.Where("owner = ?", owner.String())
	}
	if grantee.String() != "" {
		// The grantee field could also be a clique that includes this direct DID. so ifnore it
		query = query.Where("grantee = ? OR grantee LIKE ?", grantee.String(), "habitat://%") // check for the habitat uri prefix for cliques
	}
	if collection != "" {
		query = query.Where("collection = ?", collection)
	}
	if rkey != "" {
		// permissions with empty rkeys grant the entire collection
		query = query.Where("rkey = ? OR rkey = ''", rkey)
	}
	// prioritize more specific permission and denies
	query = query.Order("LENGTH(rkey) DESC, effect DESC")

	if err := query.Find(&queried).Error; err != nil {
		return nil, fmt.Errorf("failed to query permissions: %w", err)
	}

	permissions := make([]Permission, len(queried))
	for i, p := range queried {
		grantee, err := ParseGranteeFromString(p.Grantee)
		if err != nil {
			return nil, fmt.Errorf("error parsing found grantee in db: %s", grantee)
		}
		permissions[i] = Permission{
			Owner:      syntax.DID(p.Owner),
			Grantee:    grantee,
			Collection: syntax.NSID(p.Collection),
			Rkey:       syntax.RecordKey(p.Rkey),
			Effect:     Effect(p.Effect),
		}
	}
	if collection != "" && (owner == grantee || owner == "") {
		// If the request is for a general collection (un-ownered), by default the grantee has permission to their own collections.
		permissions = append(permissions, Permission{Grantee: DIDGrantee(grantee), Owner: grantee, Collection: collection, Effect: Allow})
	}
	return permissions, nil
}
