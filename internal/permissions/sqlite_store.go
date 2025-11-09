package permissions

import (
	"fmt"

	"gorm.io/gorm"
)

type sqliteStore struct {
	db *gorm.DB
}

var _ Store = (*sqliteStore)(nil)

// Permission represents a permission entry in the database
type Permission struct {
	gorm.Model
	Grantee string `gorm:"not null;index:idx_permissions_grantee_owner,priority:1;uniqueIndex:idx_grantee_owner_object"`
	Owner   string `gorm:"not null;index:idx_permissions_owner;index:idx_permissions_grantee_owner,priority:2;uniqueIndex:idx_grantee_owner_object"`
	Object  string `gorm:"not null;uniqueIndex:idx_grantee_owner_object"`
	Effect  string `gorm:"not null;check:effect IN ('allow', 'deny')"`
}

// NewSQLiteStore creates a new SQLite-backed permission store.
// The store manages permissions at different granularities:
// - Whole NSID prefixes: "com.habitat.*"
// - Specific NSIDs: "com.habitat.collection"
// - Specific records: "com.habitat.collection.recordKey"
func NewSQLiteStore(db *gorm.DB) (*sqliteStore, error) {
	// AutoMigrate will create the table with all indexes defined in the Permission struct
	err := db.AutoMigrate(&Permission{})
	if err != nil {
		return nil, fmt.Errorf("failed to migrate permissions table: %w", err)
	}

	return &sqliteStore{db: db}, nil
}

// HasPermission checks if a requester has permission to access a specific record.
// It checks permissions in the following order:
// 1. Owner always has access
// 2. Specific record permissions (exact match)
// 3. NSID-level permissions (prefix match with .*)
// 4. Wildcard prefix permissions (e.g., "com.habitat.*")
func (s *sqliteStore) HasPermission(
	requester string,
	owner string,
	nsid string,
	rkey string,
) (bool, error) {
	// Owner always has permission
	if requester == owner {
		return true, nil
	}

	// Build the full object path
	object := nsid
	if rkey != "" {
		object = fmt.Sprintf("%s.%s", nsid, rkey)
	}

	// Check for permissions using a single query that matches:
	// 1. Exact object match: object = "com.habitat.posts.record1"
	// 2. Prefix matches for parent NSIDs:
	//    For object = "com.habitat.posts.record1", match stored permissions:
	//    - "com.habitat.posts" (the NSID itself)
	//    - "com.habitat"
	//    - "com"
	//    This works by checking if the object LIKE the stored permission + ".%"
	var permission Permission
	err := s.db.Where("grantee = ? AND owner = ? AND (object = ? OR ? LIKE object || '.%')",
		requester, owner, object, object).
		Order("LENGTH(object) DESC, effect DESC").
		Limit(1).
		First(&permission).Error

	if err == gorm.ErrRecordNotFound {
		// No permission found, deny by default
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("failed to query permission: %w", err)
	}

	return permission.Effect == "allow", nil
}

// AddLexiconReadPermission grants read permission for an entire lexicon (NSID).
// The permission is stored as just the NSID (e.g., "com.habitat.posts").
// The HasPermission method will automatically check for both exact matches and wildcard patterns.
func (s *sqliteStore) AddLexiconReadPermission(
	grantee string,
	owner string,
	nsid string,
) error {
	permission := Permission{
		Grantee: grantee,
		Owner:   owner,
		Object:  nsid,
		Effect:  "allow",
	}

	// Use gorm.G for the generic GORM wrapper if available, or direct DB methods
	result := s.db.Where("grantee = ? AND owner = ? AND object = ?", grantee, owner, nsid).
		Assign(Permission{Effect: "allow"}).
		FirstOrCreate(&permission)

	if result.Error != nil {
		return fmt.Errorf("failed to add lexicon permission: %w", result.Error)
	}
	return nil
}

// RemoveLexiconReadPermission removes read permission for an entire lexicon.
func (s *sqliteStore) RemoveLexiconReadPermission(
	grantee string,
	owner string,
	nsid string,
) error {
	result := s.db.Where("grantee = ? AND owner = ? AND object = ?", grantee, owner, nsid).
		Delete(&Permission{})

	if result.Error != nil {
		return fmt.Errorf("failed to remove lexicon permission: %w", result.Error)
	}
	return nil
}

// ListReadPermissionsByLexicon returns a map of lexicon NSIDs to lists of grantees
// who have permission to read that lexicon.
func (s *sqliteStore) ListReadPermissionsByLexicon(owner string) (map[string][]string, error) {
	var permissions []Permission
	err := s.db.Where("owner = ? AND effect = ?", owner, "allow").
		Find(&permissions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query permissions: %w", err)
	}

	result := make(map[string][]string)
	for _, perm := range permissions {
		// The object is stored as the NSID itself (e.g., "com.habitat.posts")
		// So we can use it directly as the lexicon
		result[perm.Object] = append(result[perm.Object], perm.Grantee)
	}

	return result, nil
}

// ListReadPermissionsByUser returns the allow and deny lists for a specific user
// for a given NSID. This is used to filter records when querying.
func (s *sqliteStore) ListReadPermissionsByUser(
	owner string,
	requester string,
	nsid string,
) ([]string, []string, error) {
	// Query all permissions for this grantee/owner combination
	// that could match the given NSID
	// We need to check:
	// 1. Exact match: object = "nsid"
	// 2. Parent prefix that matches: nsid LIKE object || ".%"
	var permissions []Permission
	err := s.db.Where("grantee = ? AND owner = ? AND (object = ? OR ? LIKE object || '.%')",
		requester, owner, nsid, nsid).
		Find(&permissions).Error
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query permissions: %w", err)
	}

	allows := []string{}
	denies := []string{}

	for _, perm := range permissions {
		switch perm.Effect {
		case "allow":
			allows = append(allows, perm.Object)
		case "deny":
			denies = append(denies, perm.Object)
		}
	}

	return allows, denies, nil
}
