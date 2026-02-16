package permissions

import (
	"fmt"
	"strings"

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
		nsid string,
	) error
	RemoveReadPermission(
		grantee string,
		owner string,
		nsid string,
	) error
	ListReadPermissionsByLexicon(owner string) (map[string][]string, error)
	ListReadPermissionsByUser(
		owner string,
		requester string,
		nsid string,
	) (allow []string, deny []string, err error)
	ListFullAccessOwnersForGranteeCollection(grantee string, collection string) ([]string, error)
	ListSpecificRecordsForGranteeCollection(grantee string, collection string) ([]RecordPermission, error)
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
	gorm.Model
	Grantee string `gorm:"not null;index:idx_permissions_grantee_owner,priority:1;uniqueIndex:idx_grantee_owner_object"`
	Owner   string `gorm:"not null;index:idx_permissions_owner;index:idx_permissions_grantee_owner,priority:2;uniqueIndex:idx_grantee_owner_object"`
	Object  string `gorm:"not null;uniqueIndex:idx_grantee_owner_object"`
	Effect  string `gorm:"not null;check:effect IN ('allow', 'deny')"`
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
// 4. Wildcard prefix permissions (e.g., "network.habitat.*")
func (s *store) HasPermission(
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
	// 1. Exact object match: object = "network.habitat.posts.record1"
	// 2. Prefix matches for parent NSIDs:
	//    For object = "network.habitat.posts.record1", match stored permissions:
	//    - "network.habitat.posts" (the NSID itself)
	//    - "network.habitat"
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
// The permission is stored as just the NSID (e.g., "network.habitat.posts").
// The HasPermission method will automatically check for both exact matches and wildcard patterns.
func (s *store) AddReadPermission(
	grantees []string,
	owner string,
	nsid string,
) error {
	permissions := []Permission{}
	for _, grantee := range grantees {
		permissions = append(permissions, Permission{
			Grantee: grantee,
			Owner:   owner,
			Object:  nsid,
			Effect:  "allow",
		})
	}

	// Upsert: insert or update on conflict
	result := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "grantee"}, {Name: "owner"}, {Name: "object"}},
		DoUpdates: clause.AssignmentColumns([]string{"effect"}),
	}).Create(&permissions)

	if result.Error != nil {
		return fmt.Errorf("failed to add lexicon permission: %w", result.Error)
	}
	return nil
}

// RemoveLexiconReadPermission removes read permission for an entire lexicon.
func (s *store) RemoveReadPermission(
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
		result[perm.Object] = append(result[perm.Object], perm.Grantee)
	}

	return result, nil
}

// ListReadPermissionsByUser returns the allow and deny lists for a specific user
// for a given NSID. This is used to filter records when querying.
func (s *store) ListReadPermissionsByUser(
	owner string,
	requester string,
	nsid string,
) ([]string, []string, error) {
	if requester == owner {
		return []string{fmt.Sprintf("%s.*", nsid)}, []string{}, nil
	}
	// Query all permissions for this grantee/owner combination
	// that could match the given NSID
	// object = nsid OR object = nsid.*
	var permissions []Permission
	err := s.db.Where("grantee = ?", requester).
		Where("owner = ?", owner).
		Where(
			s.db.Where("object = ?", nsid).Or("object LIKE ? || '.%'", nsid),
		).
		Find(&permissions).
		Error
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

// ListFullAccessOwnersForGranteeCollection returns a list of all users (owners) who have granted full collection access
// to the grantee. This includes:
// - Exact collection match: object = collection
// - Parent wildcard match: object LIKE '%.*' AND collection LIKE prefix || '.%'
func (s *store) ListFullAccessOwnersForGranteeCollection(grantee string, collection string) ([]string, error) {
	// Query permissions where:
	// - Exact collection match: object = collection
	// - OR parent wildcard: object LIKE '%.*' AND collection LIKE SUBSTR(object, 1, LENGTH(object) - 2) || '.%'
	var perms []Permission
	err := s.db.Where("grantee = ?", grantee).
		Where("effect = ?", "allow").
		Where(
			"object = ? OR (object LIKE '%.*' AND ? LIKE SUBSTR(object, 1, LENGTH(object) - 2) || '.%')",
			collection, collection,
		).
		Find(&perms).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query full access permissions: %w", err)
	}

	owners := make([]string, 0, len(perms))
	for _, perm := range perms {
		owners = append(owners, perm.Owner)
	}

	return owners, nil
}

// ListSpecificRecordsForGranteeCollection returns a list of records that have been given direct permission
// to the grantee. This includes permissions where object LIKE collection || '.%' (specific records like "collection.rkey")
func (s *store) ListSpecificRecordsForGranteeCollection(grantee string, collection string) ([]RecordPermission, error) {
	// Query permissions where object LIKE collection || '.%' (specific records like "collection.rkey")
	var perms []Permission
	err := s.db.Where("grantee = ?", grantee).
		Where("effect = ?", "allow").
		Where("object LIKE ? || '.%'", collection).
		Find(&perms).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query specific record permissions: %w", err)
	}

	records := make([]RecordPermission, 0, len(perms))
	for _, perm := range perms {
		// Extract rkey from "collection.rkey"
		rkey := strings.TrimPrefix(perm.Object, collection+".")
		records = append(records, RecordPermission{
			Owner: perm.Owner,
			Rkey:  rkey,
		})
	}

	return records, nil
}
