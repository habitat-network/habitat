package permissions

import (
	"database/sql"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/eagraf/habitat-new/util"
	_ "github.com/mattn/go-sqlite3"
)

type sqliteStore struct {
	db *sql.DB
}

var _ Store = (*sqliteStore)(nil)

// NewSQLiteStore creates a new SQLite-backed permission store.
// The store manages permissions at different granularities:
// - Whole NSID prefixes: "com.habitat.*"
// - Specific NSIDs: "com.habitat.collection"
// - Specific records: "com.habitat.collection.recordKey"
func NewSQLiteStore(db *sql.DB) (*sqliteStore, error) {
	// Create permissions table if it doesn't exist
	// Schema: (grantee, owner, object, effect)
	// - grantee: user/group being granted permission
	// - owner: owner of the resource
	// - object: the resource pattern (NSID or NSID.recordKey)
	// - effect: "allow" or "deny"
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS permissions (
		grantee TEXT NOT NULL,
		owner TEXT NOT NULL,
		object TEXT NOT NULL,
		effect TEXT NOT NULL CHECK(effect IN ('allow', 'deny')),
		PRIMARY KEY(grantee, owner, object)
	);`)
	if err != nil {
		return nil, fmt.Errorf("failed to create permissions table: %w", err)
	}

	// Create index for faster lookups by owner
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_permissions_owner
		ON permissions(owner);`)
	if err != nil {
		return nil, fmt.Errorf("failed to create owner index: %w", err)
	}

	// Create index for faster lookups by grantee+owner
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_permissions_grantee_owner
		ON permissions(grantee, owner);`)
	if err != nil {
		return nil, fmt.Errorf("failed to create grantee_owner index: %w", err)
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
	var effect string
	query := sq.Select("effect").
		From("permissions").
		Where(sq.And{
			sq.Eq{"grantee": requester},
			sq.Eq{"owner": owner},
			sq.Or{
				sq.Eq{"object": object},
				sq.Expr("? LIKE object || '.%'", object),
			},
		}).
		OrderBy("LENGTH(object) DESC, effect DESC").
		Limit(1)

	err := query.RunWith(s.db).QueryRow().Scan(&effect)
	if err == sql.ErrNoRows {
		// No permission found, deny by default
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("failed to query permission: %w", err)
	}

	return effect == "allow", nil
}

// AddLexiconReadPermission grants read permission for an entire lexicon (NSID).
// The permission is stored as just the NSID (e.g., "com.habitat.posts").
// The HasPermission method will automatically check for both exact matches and wildcard patterns.
func (s *sqliteStore) AddLexiconReadPermission(
	grantee string,
	owner string,
	nsid string,
) error {
	_, err := sq.Insert("permissions").
		Columns("grantee", "owner", "object", "effect").
		Values(grantee, owner, nsid, "allow").
		Suffix("ON CONFLICT(grantee, owner, object) DO UPDATE SET effect = 'allow'").
		RunWith(s.db).
		Exec()
	if err != nil {
		return fmt.Errorf("failed to add lexicon permission: %w", err)
	}
	return nil
}

// RemoveLexiconReadPermission removes read permission for an entire lexicon.
func (s *sqliteStore) RemoveLexiconReadPermission(
	grantee string,
	owner string,
	nsid string,
) error {
	_, err := sq.Delete("permissions").
		Where(sq.Eq{
			"grantee": grantee,
			"owner":   owner,
			"object":  nsid,
		}).
		RunWith(s.db).
		Exec()
	if err != nil {
		return fmt.Errorf("failed to remove lexicon permission: %w", err)
	}
	return nil
}

// ListReadPermissionsByLexicon returns a map of lexicon NSIDs to lists of grantees
// who have permission to read that lexicon.
func (s *sqliteStore) ListReadPermissionsByLexicon(owner string) (map[string][]string, error) {
	rows, err := sq.Select("object", "grantee").
		From("permissions").
		Where(sq.Eq{
			"owner":  owner,
			"effect": "allow",
		}).
		RunWith(s.db).
		Query()
	if err != nil {
		return nil, fmt.Errorf("failed to query permissions: %w", err)
	}
	defer util.Close(rows)

	result := make(map[string][]string)
	for rows.Next() {
		var object, grantee string
		if err := rows.Scan(&object, &grantee); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// The object is stored as the NSID itself (e.g., "com.habitat.posts")
		// So we can use it directly as the lexicon
		result[object] = append(result[object], grantee)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
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
	query := sq.Select("object", "effect").
		From("permissions").
		Where(sq.And{
			sq.Eq{"grantee": requester},
			sq.Eq{"owner": owner},
			sq.Or{
				sq.Eq{"object": nsid},
				sq.Expr("? LIKE object || '.%'", nsid),
			},
		})

	rows, err := query.RunWith(s.db).Query()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query permissions: %w", err)
	}
	defer util.Close(rows)

	allows := []string{}
	denies := []string{}

	for rows.Next() {
		var object, effect string
		if err := rows.Scan(&object, &effect); err != nil {
			return nil, nil, fmt.Errorf("failed to scan row: %w", err)
		}

		switch effect {
		case "allow":
			allows = append(allows, object)
		case "deny":
			denies = append(denies, object)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return allows, denies, nil
}
