package permissions

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Store interface {
	HasPermission(
		requester string,
		owner string,
		nsid string,
		rkey string,
	) (bool, error)
	AddReadPermission(
		grantees []string,
		owner string,
		collection string,
		rkey string,
	) error
	RemoveReadPermission(
		grantee string,
		owner string,
		collection string,
		rkey string,
	) error
	ListReadPermissionsByLexicon(owner string) (map[string][]string, error)
	ListReadPermissionsByGrantee(grantee string, collection string) ([]Permission, error)
}

// RecordPermission represents a specific record permission (owner + rkey)
type RecordPermission struct {
	Owner string
	Rkey  string
}

type store struct {
	db *gorm.DB
}

var _ Store = (*store)(nil)

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
// It checks permissions in the following order:
// 1. Owner always has access
// 2. Specific record permissions (exact match)
// 3. NSID-level permissions (prefix match with .*)
func (s *store) HasPermission(
	requester string,
	owner string,
	collection string,
	rkey string,
) (bool, error) {
	// Owner always has permission
	if requester == owner {
		return true, nil
	}
	var permission Permission
	err := s.db.Where("grantee = ?", requester).
		Where("owner = ?", owner).
		Where("collection = ?", collection).
		// permissions with empty rkeys grant the entire collection
		Where("rkey = ? OR rkey = ''", rkey).
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
func (s *store) AddReadPermission(
	grantees []string,
	owner string,
	collection string,
	rkey string,
) error {
	if collection == "" {
		return fmt.Errorf("collection is required")
	}
	permissions := []Permission{}
	for _, grantee := range grantees {
		permissions = append(permissions, Permission{
			Grantee:    grantee,
			Owner:      owner,
			Effect:     "allow",
			Collection: collection,
			Rkey:       rkey,
		})
	}
	// Upsert: insert or update on conflict
	result := s.db.Clauses(clause.OnConflict{
		UpdateAll: true,
	}).Create(&permissions)
	if result.Error != nil {
		return fmt.Errorf("failed to add lexicon permission: %w", result.Error)
	}
	// delete redundant allow permissions
	if rkey == "" {
		result = s.db.Where("grantee IN ?", grantees).
			Where("owner = ?", owner).
			Where("rkey != ''").
			Where("effect = 'allow'").
			Delete(&Permission{})
		if result.Error != nil {
			return fmt.Errorf("failed to remove redundant allow permissions: %w", result.Error)
		}
	}
	return nil
}

// RemoveLexiconReadPermission removes read permission for an entire lexicon.
func (s *store) RemoveReadPermission(
	grantee string,
	owner string,
	collection string,
	rkey string,
) error {
	if rkey == "" {
		// delete all permissions for this collection
		result := s.db.Where("grantee = ?", grantee).
			Where("owner = ?", owner).
			Where("collection = ?", collection).
			Delete(&Permission{})
		if result.Error != nil {
			return fmt.Errorf("failed to remove collection permission: %w", result.Error)
		}
		return nil
	}
	// Check if there's a collection-level permission (empty rkey)
	var collectionPerm Permission
	err := s.db.Where("grantee = ?", grantee).
		Where("owner = ?", owner).
		Where("collection = ?", collection).
		Where("rkey = ''").
		First(&collectionPerm).
		Error
	if err == nil {
		// Collection-level permission exists, add a deny permission
		if err := s.db.Clauses(clause.OnConflict{
			UpdateAll: true,
		}).Create(&Permission{
			Grantee:    grantee,
			Owner:      owner,
			Collection: collection,
			Rkey:       rkey,
			Effect:     "deny",
		}).Error; err != nil {
			return fmt.Errorf("failed to add deny permission: %w", err)
		}
		return nil
	} else if err != gorm.ErrRecordNotFound {
		return fmt.Errorf("failed to check collection permission: %w", err)
	}

	// No collection-level permission, just delete the specific permission
	if err := s.db.Where("grantee = ?", grantee).
		Where("owner = ?", owner).
		Where("collection = ?", collection).
		Where("rkey = ?", rkey).
		Where("effect = 'allow'").
		Delete(&Permission{}).Error; err != nil {
		return fmt.Errorf("failed to remove permission: %w", err)
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

// ListReadPermissionsByUser returns the allow and deny lists for a specific user
// for a given NSID. This is used to filter records when querying.
func (s *store) ListReadPermissionsByGrantee(
	grantee string,
	collection string,
) ([]Permission, error) {
	// Query all permissions for this grantee/owner combination
	// that could match the given NSID
	// object = nsid OR object = nsid.*
	var permissions []Permission
	query := s.db.Where("grantee = ?", grantee)
	if collection != "" {
		query = query.Where("collection = ?", collection)
	}
	// sort permissions by collection and rkey so that empty values appear first
	if err := query.Order("collection").Order("rkey").Find(&permissions).Error; err != nil {
		return nil, fmt.Errorf("failed to query permissions: %w", err)
	}
	return append(
		[]Permission{{Grantee: grantee, Owner: grantee, Collection: collection, Effect: "allow"}},
		permissions...), nil
}
