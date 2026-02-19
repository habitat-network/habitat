package permissions

import (
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xmaps"
	"github.com/bradenaw/juniper/xslices"
	"github.com/habitat-network/habitat/internal/xrpcchannel"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Enforcer interface {
	// TODO: should we strongly type all of this stuff?
	HasPermission(
		requester syntax.DID,
		owner syntax.DID,
		collection syntax.NSID,
		rkey syntax.RecordKey,
	) (bool, error)
	AddReadPermission(
		grantees []Grantee,
		owner string,
		collection string,
		rkey string,
	) error
	RemoveReadPermissions(
		grantee []Grantee,
		owner string,
		collection string,
		rkey string,
	) error
	ListReadPermissionsByLexicon(owner string) (map[string][]string, error)
	ListReadPermissionsByUser(grantee syntax.DID, collection string) ([]Permission, error)
}

// RecordPermission represents a specific record permission (owner + rkey)
type RecordPermission struct {
	Owner string
	Rkey  string
}

type store struct {
	// Backing data store for some subset of data that this enforcer
	db *gorm.DB

	xrpcCh xrpcchannel.XrpcChannel
}

var _ Enforcer = (*store)(nil)

// Permission represents a permission entry in the database
type Permission struct {
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
	err := db.AutoMigrate(&Permission{})
	if err != nil {
		return nil, fmt.Errorf("failed to migrate permissions table: %w", err)
	}

	return &store{db: db}, nil
}

// HasPermission checks if a requester has permission to access a specific record.
func (s *store) HasPermission(
	requester syntax.DID,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) (bool, error) {
	// Owner always has permission
	if requester == owner {
		return true, nil
	}
	var permission Permission
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

	return permission.Effect == "allow", nil
}

// AddLexiconReadPermission grants read permission for an entire collection or specific record.
// If rkey is empty, it grants read permission for the whole collection.
// Will delete redundant permissions that are covered by the new grant.
// Will not add redundant permssions that are less powerful than existing ones
// Will not error in cases where no work is done
func (s *store) AddReadPermission(
	granteesTyped []Grantee,
	owner string,
	collection string,
	rkey string,
) error {
	if collection == "" {
		return fmt.Errorf("collection is required")
	}

	grantees := xslices.Map(granteesTyped, func(g Grantee) string {
		return g.String()
	})

	var existingCollectionPermissions []Permission
	if rkey == "" { /* collection-level permission */
		// delete redundant allow permissions that are less powerful
		result := s.db.Where("grantee IN ?", grantees).
			Where("owner = ?", owner).
			Where("rkey != ''").
			Where("effect = 'allow'").
			Delete(&Permission{})
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
	permissions := []Permission{}
	for grantee := range granteesSet {
		permissions = append(permissions, Permission{
			Grantee:    grantee,
			Owner:      owner,
			Effect:     "allow",
			Collection: collection,
			Rkey:       rkey,
		})
	}
	// upsert new permission
	result := s.db.Clauses(clause.OnConflict{
		UpdateAll: true,
	}).Create(&permissions)
	if result.Error != nil {
		return fmt.Errorf("failed to add lexicon permission: %w", result.Error)
	}
	return nil
}

// RemoveLexiconReadPermission removes read permission for an entire collection or specific record.
// If rkey is empty, it revokes read permission for the whole collection.
// If the user already has access to the collection, it will add a deny permission for rkey.
// Otherwise will delete the specific permission if it exists.
// Will not error if there are no permissions to remove.
func (s *store) RemoveReadPermissions(
	granteesTyped []Grantee,
	owner string,
	collection string,
	rkey string,
) error {
	grantees := xslices.Map(granteesTyped, func(g Grantee) string {
		return g.String()
	})
	// If the removal is on an entire collection, delete all allow permissions for records in the collection or the entire collection.
	if rkey == "" {
		// delete all permissions for this collection
		result := s.db.Where("grantee IN ?", grantees).
			Where("owner = ?", owner).
			Where("collection = ?", collection).
			Delete(&Permission{})
		if result.Error != nil {
			return fmt.Errorf("failed to remove collection permission: %w", result.Error)
		}
		return nil
	}

	// Otherwise, the removal is on a specific record key, so add a deny effect on the specific record for a batch of grantees.
	recordPerms := make([]*Permission, len(grantees))
	for i, grantee := range grantees {
		recordPerms[i] = &Permission{
			Grantee:    grantee,
			Owner:      owner,
			Collection: collection,
			Rkey:       rkey,
			Effect:     "deny",
		}
	}

	// Collection-level permission exists, add a deny permission
	if err := s.db.Clauses(clause.OnConflict{
		UpdateAll: true,
	}).Create(recordPerms).Error; err != nil {
		return fmt.Errorf("failed to add deny permissions: %w", err)
	}
	return nil
}

// ListReadPermissionsByLexicon returns a map of lexicon NSIDs to lists of grantees
// who have permission to read that lexicon.
func (s *store) ListReadPermissionsByLexicon(owner string) (map[string][]string, error) {
	var permissions []Permission
	err := s.db.Where("owner = ? AND effect = ?", owner, "allow").
		Find(&permissions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query permissions: %w", err)
	}

	result := make(map[string][]string)
	for _, perm := range permissions {
		// The object is stored as the NSID itself (e.g., "network.habitat.posts")
		// So we can use it directly as the lexicon
		object := perm.Collection
		if perm.Rkey != "" {
			object = fmt.Sprintf("%s.%s", object, perm.Rkey)
		}
		result[object] = append(result[object], perm.Grantee)
	}
	return result, nil
}

// ListReadPermissionsByUser returns the permissions available to a grantee.
// If a collection is provided, only permissions for that collection are returned.
func (s *store) ListReadPermissionsByUser(
	grantee syntax.DID,
	collection string,
) ([]Permission, error) {
	var permissions []Permission
	query := s.db.Where("grantee = ?", grantee.String())
	if collection != "" {
		query = query.Where("collection = ?", collection)
	}
	if err := query.Find(&permissions).Error; err != nil {
		return nil, fmt.Errorf("failed to query permissions: %w", err)
	}
	return append(
		// grant the owner access to all of their own permissions
		[]Permission{{Grantee: grantee.String(), Owner: grantee.String(), Collection: collection, Effect: "allow"}},
		// append all others
		permissions...), nil
}
