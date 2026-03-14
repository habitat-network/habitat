package permissions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xmaps"
	"github.com/bradenaw/juniper/xslices"
	"github.com/habitat-network/habitat/internal/clique"
	"github.com/habitat-network/habitat/internal/node"
	"gorm.io/gorm"

	habitat_err "github.com/habitat-network/habitat/internal/error"
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
	ResolvePermissionsForCollection(
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

	// The store needs to know which DIDs it serves and possibly route requests to other nodes in order resolve permissions.
	node node.Node

	// To look up cliques
	cliqueStore clique.Store
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
func NewStore(db *gorm.DB, node node.Node) (*store, error) {
	// AutoMigrate will create the table with all indexes defined in the Permission struct
	err := db.AutoMigrate(&permission{})
	if err != nil {
		return nil, fmt.Errorf("failed to migrate permissions table: %w", err)
	}

	return &store{db: db, node: node}, nil
}

func (s *store) isRemoteCliqueMember(ctx context.Context, callerDID syntax.DID, clique habitat_syntax.Clique, did syntax.DID) (bool, error) {
	panic("unimplemented")
}

func (s *store) isCliqueMember(ctx context.Context, requester syntax.DID, clique habitat_syntax.Clique) (bool, error) {
	panic("unimplemented")
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

	// The clique collection is reserved for special cases by the server.
	if collection == habitat_syntax.ReservedCliqueNSID {
		return false, habitat_err.ErrNoGettingClique
	}

	permissions, err := s.listPermissions(requester, []syntax.DID{owner}, collection, rkey)
	if err != nil {
		return false, err
	}

	localCliques := []habitat_syntax.Clique{}
	remoteCliques := []habitat_syntax.Clique{}
	for _, permission := range permissions {
		switch grantee := permission.Grantee.(type) {
		case DIDGrantee:
			// Direct grants to this DID can immediately resolve this request.
			if grantee.String() != requester.String() {
				// This should never happen, so return an error
				// We need to log / measure this
				return false, fmt.Errorf("invalid permisssion returned from ListPermissions")
			}
			// Explicit allow or deny for this DID resolves the request
			if permission.Effect == Allow {
				return true, nil
			} else {
				return false, nil
			}
		case habitat_syntax.Clique:
			// Otherwise an indirection for looking up cliques may need to happen.
			ok, err := s.node.ServesDID(ctx, grantee.Authority())
			if err != nil {
				return false, err
			}

			ok, err = s.isCliqueMember(ctx, requester, grantee)
			if err != nil {
				return false, err
			}

			if ok {
				return true, nil
			}
		}
	}

	// First try to resolve local cliques
	if len(localCliques) > 0 {
		s.cliqueStore.IsAnyCliqueMember(localCliques, requester)
	}

	// TODO: clique store should be able to resolve local / remoteness itself
	if len(remoteCliques) > 0 {
		var remoteErr error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Resolve remote cliques
			// TODO: batch for better performance?
			for _, clique := range remoteCliques {
				ok, err := s.isRemoteCliqueMember(ctx, owner /* the request originates from the record owner */, clique, requester /* checking membership on the requester */)
				if err == nil && ok {
					// Success, membership found, allow permission
					return true, nil
				} else if err != nil {
					// Arbitrarily save the latest error, but continue trying to resolve all memberships in case one succeeds
					remoteErr = err
				}
			}
		}

		return false, remoteErr
	}

	// Default = deny
	return false, nil
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

	// Delete any existing permission for this record before inserting the deny.
	// This is jank and its because SQLITE/postgres differ in the ON CONFLICT specs. We should fix this.
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("grantee IN ?", grantees).
			Where("owner = ?", owner).
			Where("collection = ?", collection).
			Where("rkey = ?", rkey).
			Delete(&permission{}).Error; err != nil {
			return fmt.Errorf("failed to clear existing permissions before deny insert: %w", err)
		}
		if err := tx.Create(&permissions).Error; err != nil {
			return fmt.Errorf("failed to add lexicon permission: %w", err)
		}
		return nil
	})
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

	// Otherwise, the removal is on a specific record key.
	return s.db.Transaction(func(tx *gorm.DB) error {

		// Delete any existing allows on this record.
		err := tx.Where("grantee IN ?", grantees).
			Where("owner = ?", owner).
			Where("collection = ?", collection).
			Where("rkey = ?", rkey).
			Where("effect = ?", Allow).
			Delete(&permission{}).Error
		if err != nil {
			return fmt.Errorf("failed to remove allow permission: %w", err)
		}

		// For any grantees that have permissions to the whole collection, add a deny
		var denyGrantees []string
		err = tx.Model(&permission{}).
			Where("grantee IN ?", grantees).
			Where("owner = ?", owner).
			Where("collection = ?", collection).
			Where("rkey = ''").
			Where("effect = ?", Allow).
			Pluck("grantee", &denyGrantees).
			Error
		if err != nil {
			return fmt.Errorf("failed to identify collection-level permissions: %w", err)
		}

		if len(denyGrantees) == 0 {
			return nil
		}

		// Only need to add a deny row for people with collection-level permissions
		recordPerms := make([]*permission, len(grantees))
		for i, grantee := range denyGrantees {
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
		if err := tx.Where("grantee IN ?", denyGrantees).
			Where("owner = ?", owner).
			Where("collection = ?", collection).
			Where("rkey = ?", rkey).
			Delete(&permission{}).Error; err != nil {
			return fmt.Errorf("failed to clear existing permissions before deny insert: %w", err)
		}
		if err := tx.Create(recordPerms).Error; err != nil {
			return fmt.Errorf("failed to add deny permissions: %w", err)
		}
		return nil
	})
}

func (s *store) ResolvePermissionsForCollection(ctx context.Context, grantee syntax.DID, collection syntax.NSID, owners []syntax.DID) ([]Permission, error) {
	allPermissions, err := s.listPermissions(grantee, owners, collection, "")
	if err != nil {
		return nil, err
	}

	relevant := []Permission{}
	for _, permission := range allPermissions {
		clique, ok := permission.Grantee.(habitat_syntax.Clique)
		if !ok {
			// Directly return specific grants for this DID
			relevant = append(relevant, permission)
			continue
		}

		// Otherwise, it's a clique grantee, so we need to resolve it
		// TODO: we could potentially be more efficient with the DB query than resolving each independently.
		ok, err = s.isCliqueMember(ctx, grantee, clique)
		if err != nil {
			return nil, err
		}

		if ok {
			// Keep all other fields of the permission the same
			permission.Grantee = DIDGrantee(grantee)
			relevant = append(relevant, permission)
		}
		// If this did is not a member of the clique, ignore this permission
	}

	return relevant, nil
}

// ListPermissionGrants implements Store.
func (s *store) ListPermissionGrants(ctx context.Context, granter syntax.DID, collection syntax.NSID) ([]Permission, error) {
	return s.listPermissions("", []syntax.DID{granter}, collection, "")
}

// ListPermissionsForRecord implements Store.
func (s *store) ListAllowedGranteesForRecord(ctx context.Context, owner syntax.DID, collection syntax.NSID, rkey syntax.RecordKey) ([]Grantee, error) {
	if rkey.String() == "" || collection.String() == "" {
		return nil, fmt.Errorf("this function expects to be called on a particular collection + record key; got collection %s, record %s", collection, rkey)
	}

	permissions, err := s.listPermissions("", []syntax.DID{owner}, collection, rkey)
	if err != nil {
		return nil, err
	}

	denied := make(xmaps.Set[Grantee])
	for _, p := range permissions {
		if p.Effect == Deny {
			denied.Add(p.Grantee)
		}
	}

	var allowed []Grantee
	for _, p := range permissions {
		if p.Effect == Allow && !denied.Contains(p.Grantee) {
			allowed = append(allowed, p.Grantee)
		}
	}

	return allowed, nil
}

// ListPermissions returns the permissions available to this particular combination of inputs.
// Any "" inputs are not filtered by.
func (s *store) listPermissions(
	grantee syntax.DID,
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
	if collection != "" && (slices.Contains(owners, grantee) || len(owners) == 0) {
		// If the request is for a general collection (un-ownered), by default the grantee has permission to their own collections.
		permissions = append(permissions, Permission{Grantee: DIDGrantee(grantee), Owner: grantee, Collection: collection, Effect: Allow})
	}
	return permissions, nil
}
