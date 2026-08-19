package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var ErrSpaceAlreadyExists = errors.New("space already exists for this clique")

// CliqueMembersReader reads members of a clique.
type CliqueMembersReader interface {
	GetMembers(ctx context.Context, clique habitat_syntax.Clique) ([]syntax.DID, error)
}

// SpaceWriter creates spaces and adds members.
type SpaceWriter interface {
	CreateSpace(
		ctx context.Context,
		owner syntax.DID,
		spaceType syntax.NSID,
		skey habitat_syntax.SpaceKey,
	) (habitat_syntax.SpaceURI, error)
	AddMember(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		did syntax.DID,
		access spaces.SpaceAccess,
	) error
}

// MigrateCliques finds all cliques referenced in permissions for
// network.habitat.docs or network.habitat.docs.edit, creates a space for
// each, and adds the clique's members as space members.
// Records are copied into the space with collection set to network.habitat.docs.edit.
func MigrateCliques(
	ctx context.Context,
	db *gorm.DB,
	cliques CliqueMembersReader,
	spaces SpaceWriter,
) (int, error) {
	rows, err := db.Raw(`
		SELECT DISTINCT grantee FROM permissions
		WHERE collection IN (?, ?)
		AND grantee LIKE ?
	`, "network.habitat.docs", "network.habitat.docs.edit", "clique:%").Rows()
	if err != nil {
		return 0, fmt.Errorf("query cliques: %w", err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		var grantee string
		if err := rows.Scan(&grantee); err != nil {
			return count, fmt.Errorf("scan grantee: %w", err)
		}

		_, err := migrateOneClique(ctx, db, cliques, spaces, grantee)
		if errors.Is(err, ErrSpaceAlreadyExists) {
			spaceURI := spaceURIFromGrantee(grantee)
			if copyErr := copyRecords(db, grantee, spaceURI); copyErr != nil {
				log.Printf("WARN: copying records for %q: %v", grantee, copyErr)
			}
			continue
		}
		if err != nil {
			log.Printf("WARN: skipping clique %q: %v", grantee, err)
			continue
		}
		count++
	}
	return count, nil
}

func spaceURIFromGrantee(grantee string) habitat_syntax.SpaceURI {
	c, err := habitat_syntax.ParseClique(grantee)
	if err != nil {
		return ""
	}
	return habitat_syntax.ConstructSpaceURI(
		c.Authority(),
		"network.habitat.docs",
		habitat_syntax.SpaceKey(c.Key()),
	)
}

func migrateOneClique(
	ctx context.Context,
	db *gorm.DB,
	cliques CliqueMembersReader,
	spaceStore SpaceWriter,
	grantee string,
) (habitat_syntax.SpaceURI, error) {
	clique, err := habitat_syntax.ParseClique(grantee)
	if err != nil {
		return "", fmt.Errorf("parse clique %q: %w", grantee, err)
	}

	owner := clique.Authority()
	key := clique.Key()

	// Check if space already exists for this owner+key.
	var existing int64
	db.Model(&spaceRow{}).Where("owner = ? AND skey = ?", owner.String(), key).Count(&existing)
	if existing > 0 {
		return "", ErrSpaceAlreadyExists
	}

	members, err := cliques.GetMembers(ctx, clique)
	if err != nil {
		return "", fmt.Errorf("get members for %q: %w", grantee, err)
	}

	spaceURI, err := spaceStore.CreateSpace(
		ctx,
		owner,
		"network.habitat.docs",
		habitat_syntax.SpaceKey(key),
	)
	if err != nil {
		return "", fmt.Errorf("create space for %q: %w", grantee, err)
	}
	log.Printf("created space %s for clique %s", spaceURI, grantee)

	for _, member := range members {
		if member == owner {
			continue
		}
		if err := spaceStore.AddMember(ctx, spaceURI, member, spaces.SpaceAccessWrite); err != nil {
			return "", fmt.Errorf("add member %s to space %s: %w", member, spaceURI, err)
		}
	}
	log.Printf("added %d members to space %s", len(members), spaceURI)

	if err := copyRecords(db, grantee, spaceURI); err != nil {
		log.Printf("WARN: copying records for %q: %v", grantee, err)
	}
	return spaceURI, nil
}

func copyRecords(db *gorm.DB, grantee string, spaceURI habitat_syntax.SpaceURI) error {
	insertPrefix := "INSERT OR IGNORE INTO"
	if db.Dialector.Name() == "postgres" {
		insertPrefix = "INSERT INTO"
	}
	suffix := ""
	if db.Dialector.Name() == "postgres" {
		suffix = " ON CONFLICT (space, owner, collection, rkey) DO NOTHING"
	}
	stmt := fmt.Sprintf(`
		%s space_records (space, owner, collection, rkey, value, created_at, updated_at)
		SELECT ? AS space, p.owner, ? AS collection, p.rkey, r.value,
		       COALESCE(r.created_at, CURRENT_TIMESTAMP),
		       COALESCE(r.updated_at, CURRENT_TIMESTAMP)
		FROM permissions p
		LEFT JOIN records r ON r.did = p.owner AND r.collection = p.collection AND r.rkey = p.rkey
		WHERE p.grantee = ?
		%s
	`, insertPrefix, suffix)
	editCollection := "network.habitat.docs.edit"
	res := db.Exec(stmt, spaceURI.String(), editCollection, grantee)
	if res.Error != nil {
		return fmt.Errorf("copy records for %q: %w", grantee, res.Error)
	}
	log.Printf("copied %d records to space %s", res.RowsAffected, spaceURI)
	return nil
}

// spaceRow mirrors the spaces table for the existence check.
type spaceRow struct {
	Owner string `gorm:"primaryKey"`
	Skey  string `gorm:"primaryKey"`
}

func (spaceRow) TableName() string {
	return "spaces"
}
