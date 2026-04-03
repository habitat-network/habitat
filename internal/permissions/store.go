package permissions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xslices"
	"github.com/habitat-network/habitat/internal/clique"
	"gorm.io/gorm"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type Store interface {
	// Whether the requester is directly granted permission to this record.
	// If the requester has indirect permissions via cliques, this returns false.
	HasPermission(
		ctx context.Context,
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
	ListGranteePermissions(
		ctx context.Context,
		grantee syntax.DID,
		collection syntax.NSID,
		owners []syntax.DID,
	) ([]Permission, error)
	ListPermissionGrants(
		ctx context.Context,
		granter syntax.DID,
		collection syntax.NSID,
	) ([]Permission, error)
	ListAllowedGranteesForRecord(
		ctx context.Context,
		owner syntax.DID,
		collection syntax.NSID,
		rkey syntax.RecordKey,
	) ([]Grantee, error)
}

// RecordPermission represents a specific record permission (owner + rkey)
type RecordPermission struct {
	Owner string
	Rkey  string
}

type store struct {
	// Backing data store for some subset of data that this permissions provider has access to.
	db *gorm.DB

	// To look up cliques
	cliqueStore clique.Store
}

var _ Store = (*store)(nil)

// Public — exported for use elsewhere, typed with Grantee interface
type Permission struct {
	Grantee    Grantee
	Owner      syntax.DID
	Collection syntax.NSID
	Rkey       syntax.RecordKey
}

// Permission represents a permission entry in the database. this type defines the Gorm DB model of permissions, not what is exported to other packages.
type permission struct {
	Grantee    string `gorm:"primaryKey"`
	Owner      string `gorm:"primaryKey"`
	Collection string `gorm:"primaryKey"`
	Rkey       string `gorm:"primaryKey"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

var (
	ErrCollectionLevelNotSupported = errors.New("collection-level permissions are not supported")
)

// NewStore creates a new db-backed permission store.
// The store manages permissions at different granularities:
// - Whole NSID prefixes: "network.habitat.*"
// - Specific NSIDs: "network.habitat.collection"
// - Specific records: "network.habitat.collection.recordKey"
func NewStore(db *gorm.DB, cliqueStore clique.Store) (*store, error) {
	// AutoMigrate will create the table with all indexes defined in the Permission struct
	err := db.AutoMigrate(&permission{})
	if err != nil {
		return nil, fmt.Errorf("failed to migrate permissions table: %w", err)
	}

	// Drop the effect column if it still exists
	if db.Migrator().HasColumn(&permission{}, "effect") {
		if err := db.Migrator().DropColumn(&permission{}, "effect"); err != nil {
			return nil, fmt.Errorf("failed to drop effect column: %w", err)
		} else {
			fmt.Println("dropped effect column")
		}
	}

	// Delete collection-level grants (rkey = "") — these are unsupported
	err = db.Where("rkey = ?", "").Delete(&permission{}).Error
	if err != nil {
		return nil, fmt.Errorf("failed to delete collection-level permissions: %w", err)
	}

	return &store{db: db, cliqueStore: cliqueStore}, nil
}

// HasPermission checks if a requester has permission to access a specific record.
func (s *store) HasPermission(
	ctx context.Context,
	requester syntax.DID,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) (bool, error) {
	// Owner always has permission
	if requester == owner {
		return true, nil
	}

	if rkey == "" {
		return false, ErrCollectionLevelNotSupported
	}

	// TODO: this query shouldn't use listPermissions
	var permissions []permission
	err := s.db.
		Where("grantee = ? OR grantee LIKE ?", requester.String(), "clique:%"). // check for the habitat uri prefix for cliques
		Where("owner = ?", owner).
		Where("collection = ?", collection).
		Where("rkey = ?", rkey).
		Find(&permissions).Error
	if err != nil {
		return false, err
	}

	for _, permission := range permissions {
		parsed, err := ParseGranteeFromString(permission.Grantee)
		if err != nil {
			return false, fmt.Errorf("parsing grantee from db: %s: %w", parsed, err)
		}
		switch grantee := parsed.(type) {
		case DIDGrantee:
			// Direct grants to this DID can immediately resolve this request.
			if grantee.String() != requester.String() {
				// This should never happen, so return an error
				// We need to log / measure this
				return false, fmt.Errorf("invalid permisssion returned from ListPermissions")
			}
			// Found a match, return
			return true, nil
		case habitat_syntax.Clique:
			ok, err := s.cliqueStore.IsMember(grantee, requester)
			if err != nil {
				return false, err
			}

			if ok {
				return true, nil
			}
		default:
			return false, fmt.Errorf("unknown grantee type: %+v", grantee)
		}
	}

	// Default = deny
	return false, nil
}

// AddPermissions grants read permission for an entire collection or specific record.
func (s *store) AddPermissions(
	granteesTyped []Grantee,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) error {
	if collection == "" {
		return fmt.Errorf("collection is required")
	}

	if rkey == "" {
		return ErrCollectionLevelNotSupported
	}

	grantees := xslices.Map(granteesTyped, func(g Grantee) string {
		return g.String()
	})

	// Delete any existing permission for this record before inserting the deny.
	// This is jank and its because SQLITE/postgres differ in the ON CONFLICT specs. We should fix this.
	return s.db.Transaction(func(tx *gorm.DB) error {
		var existing []string
		err := tx.Model(&permission{}).
			Where("grantee IN ?", grantees).
			Where("owner = ?", owner).
			Where("collection = ?", collection).
			Where("rkey = ?", rkey).
			Pluck("grantee", &existing).Error
		if err != nil {
			return fmt.Errorf("error querying existing grantees: %w", err)
		}

		newGrantees := slices.DeleteFunc(grantees, func(g string) bool {
			return slices.Contains(existing, g)
		})

		if len(newGrantees) == 0 {
			// Nothing to do
			return nil
		}

		permissions := make([]permission, len(newGrantees))
		for i, grantee := range newGrantees {
			permissions[i] = permission{
				Grantee:    grantee,
				Owner:      owner.String(),
				Collection: collection.String(),
				Rkey:       rkey.String(),
			}
		}

		if err := tx.Create(&permissions).Error; err != nil {
			return fmt.Errorf("failed to add lexicon permission: %w", err)
		}
		return nil
	})
}

// RemovePermissions removes read permission for a specific record and the given grantees.
func (s *store) RemovePermissions(
	granteesTyped []Grantee,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) error {
	grantees := xslices.Map(granteesTyped, func(g Grantee) string {
		return g.String()
	})

	if rkey == "" {
		return ErrCollectionLevelNotSupported
	}

	// Delete any existing allows on this record.
	return s.db.Where("grantee IN ?", grantees).
		Where("owner = ?", owner).
		Where("collection = ?", collection).
		Where("rkey = ?", rkey).
		Delete(&permission{}).Error
}

// Returns all permissions which have the given grantee for records part of collection and owned by owners
func (s *store) ListGranteePermissions(ctx context.Context, grantee syntax.DID, collection syntax.NSID, owners []syntax.DID) ([]Permission, error) {
	// Direct permission grants
	direct, err := s.listPermissions([]Grantee{DIDGrantee(grantee)}, owners, collection, "*")
	if err != nil {
		return nil, err
	}

	// Indirect permission grants through clique
	cliques, err := s.cliqueStore.GetCliquesForMember(grantee)
	if err != nil {
		return nil, err
	}

	cliqueGrantees := xslices.Map(cliques, func(c habitat_syntax.Clique) Grantee {
		return Grantee(c)
	})

	indirect, err := s.listPermissions(cliqueGrantees, owners, collection, "*")
	if err != nil {
		return nil, err
	}

	return append(direct, indirect...), nil
}

// ListPermissionGrants implements Store.
func (s *store) ListPermissionGrants(ctx context.Context, granter syntax.DID, collection syntax.NSID) ([]Permission, error) {
	return s.listPermissions([]Grantee{ /* match any */ }, []syntax.DID{granter}, collection, "")
}

// ListPermissionsForRecord implements Store.
func (s *store) ListAllowedGranteesForRecord(ctx context.Context, owner syntax.DID, collection syntax.NSID, rkey syntax.RecordKey) ([]Grantee, error) {
	if rkey.String() == "" || collection.String() == "" {
		return nil, fmt.Errorf("this function expects to be called on a particular collection + record key; got collection %s, record %s", collection, rkey)
	}

	permissions, err := s.listPermissions([]Grantee{ /* match any */ }, []syntax.DID{owner}, collection, rkey)
	if err != nil {
		return nil, err
	}

	grantees := xslices.Map(permissions, func(p Permission) Grantee {
		return p.Grantee
	})
	return grantees, nil
}

// ListPermissions returns the permissions available to this particular combination of inputs.
// Any "" inputs are not filtered by.
func (s *store) listPermissions(
	grantees []Grantee,
	owners []syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) ([]Permission, error) {
	// TODO prevent table scans if all arguments are ""
	var queried []permission
	query := s.db
	if len(owners) > 0 {
		query = query.Where("owner IN ?", owners)
	}
	if len(grantees) > 0 {
		// The grantee field could also be a clique that includes this direct DID. so ifnore it
		query = query.Where("grantee IN ?", grantees) // check for the habitat uri prefix for cliques
	}
	if collection != "" {
		query = query.Where("collection = ?", collection)
	}
	if rkey != "" {
		// permissions with empty rkeys grant the entire collection
		query = query.Where("rkey = ? OR rkey = ''", rkey)
	}

	if err := query.Find(&queried).Error; err != nil {
		return nil, fmt.Errorf("failed to query permissions: %w", err)
	}

	permissions := make([]Permission, len(queried))
	for i, p := range queried {
		var err error
		permissions[i], err = toPermission(p)
		if err != nil {
			return nil, fmt.Errorf("error formatting row: %w", err)
		}
	}
	return permissions, nil
}

func toPermission(p permission) (Permission, error) {
	grantee, err := ParseGranteeFromString(p.Grantee)
	if err != nil {
		return Permission{}, fmt.Errorf("error parsing grantee: %s", grantee)
	}
	return Permission{
		Owner:      syntax.DID(p.Owner),
		Grantee:    grantee,
		Collection: syntax.NSID(p.Collection),
		Rkey:       syntax.RecordKey(p.Rkey),
	}, nil
}
