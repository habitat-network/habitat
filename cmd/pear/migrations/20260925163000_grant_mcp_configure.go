package migrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func init() {
	goose.AddMigrationContext(upGrantMcpConfigure, downGrantMcpConfigure)
}

// upGrantMcpConfigure binds the mcp.configure action to the admin role in the
// community.opensocial.permissions record of every existing opensocial org
// that doesn't already bind it, matching the default NewOrg seeds for new orgs.
// Orgs without a permissions record are left alone: CheckAction already
// authorizes their admins for every action.
//
// The record is rewritten through the spaces store so the members space's
// cached repo hash and rev stay consistent with the record. Registered syncers
// are not notified of the write (there's no notifier while migrating); they
// pick up the new rev on their next sync.
func upGrantMcpConfigure(ctx context.Context, tx *sql.Tx) error {
	pg, err := isPostgres(ctx, tx)
	if err != nil {
		return err
	}
	// The spaces tables are created by GORM's AutoMigrate after migrations
	// run, so a fresh database has no orgs to update.
	exists, err := tableExists(ctx, tx, "space_records", pg)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	orgs, err := listOrgsWithPermissions(ctx, tx, pg)
	if err != nil {
		return err
	}
	if len(orgs) == 0 {
		return nil
	}

	gormDB, err := gormForTx(tx, pg)
	if err != nil {
		return err
	}
	store, err := spaces.NewStore(gormDB, noopNotifier{}, nil)
	if err != nil {
		return fmt.Errorf("create spaces store: %w", err)
	}
	for _, org := range orgs {
		if err := grantMcpConfigure(ctx, store, org); err != nil {
			return fmt.Errorf("grant mcp.configure to %s: %w", org, err)
		}
	}
	return nil
}

// downGrantMcpConfigure is a no-op: new orgs are seeded with the mcp.configure
// binding too, so removing it can't tell which orgs this migration touched,
// and communities may have rebound it since.
func downGrantMcpConfigure(ctx context.Context, tx *sql.Tx) error {
	return nil
}

// listOrgsWithPermissions returns the DIDs of the orgs that have written a
// permissions record to their own members space.
func listOrgsWithPermissions(ctx context.Context, tx *sql.Tx, pg bool) ([]syntax.DID, error) {
	rows, err := tx.QueryContext(ctx,
		"SELECT space, repo FROM space_records WHERE collection = "+bind(pg, 1)+
			" AND rkey = 'self' AND deleted_at IS NULL",
		opensocial.PermissionsCollection)
	if err != nil {
		return nil, fmt.Errorf("list permissions records: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var orgs []syntax.DID
	for rows.Next() {
		var space, repo string
		if err := rows.Scan(&space, &repo); err != nil {
			return nil, err
		}
		uri, err := habitat_syntax.ParseSpaceURI(space)
		if err != nil {
			return nil, fmt.Errorf("parse space %q: %w", space, err)
		}
		if uri.SpaceType() != opensocial.MembersSpaceType || uri.Skey() != "self" ||
			uri.SpaceOwner().String() != repo {
			continue
		}
		orgs = append(orgs, uri.SpaceOwner())
	}
	return orgs, rows.Err()
}

// grantMcpConfigure adds an mcp.configure binding for the admin role to
// orgDID's permissions record, unless mcp.configure is already bound.
func grantMcpConfigure(ctx context.Context, store spaces.Store, orgDID syntax.DID) error {
	membersSpace := habitat_syntax.ConstructSpaceURI(orgDID, opensocial.MembersSpaceType, "self")
	record, err := store.GetRecord(
		ctx, membersSpace, orgDID, opensocial.PermissionsCollection, "self",
	)
	if err != nil {
		return fmt.Errorf("get permissions record: %w", err)
	}
	permissions, err := decodePermissions(record.Value)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(permissions.Bindings,
		func(b opensocial_api.CommunityOpensocialPermissionsActionBinding) bool {
			return opensocial.Action(b.Action) == opensocial.ActionMcpConfigure
		}) {
		return nil
	}
	permissions.Bindings = append(permissions.Bindings,
		opensocial_api.CommunityOpensocialPermissionsActionBinding{
			Action: string(opensocial.ActionMcpConfigure),
			Roles:  []string{opensocial.AdminRoleRkey},
		})
	permissions.UpdatedAt = time.Now().Format(time.RFC3339)
	recordBytes, err := spaces.MarshalRecord(permissions)
	if err != nil {
		return fmt.Errorf("marshal permissions record: %w", err)
	}
	if _, _, err := store.PutRecord(
		ctx, membersSpace, orgDID, opensocial.PermissionsCollection, "self", recordBytes,
	); err != nil {
		return fmt.Errorf("put permissions record: %w", err)
	}
	return nil
}

// decodePermissions converts a stored record value back into its typed form,
// round-tripping through JSON as internal/opensocial does.
func decodePermissions(
	value map[string]any,
) (opensocial_api.CommunityOpensocialPermissions, error) {
	var permissions opensocial_api.CommunityOpensocialPermissions
	raw, err := json.Marshal(value)
	if err != nil {
		return permissions, fmt.Errorf("marshal permissions record: %w", err)
	}
	if err := json.Unmarshal(raw, &permissions); err != nil {
		return permissions, fmt.Errorf("decode permissions record: %w", err)
	}
	return permissions, nil
}

// gormForTx wraps the migration's transaction in a GORM DB so stores built on
// it write within that transaction. GORM nests its own transactions as
// savepoints on it.
func gormForTx(tx *sql.Tx, postgresDB bool) (*gorm.DB, error) {
	var dialector gorm.Dialector
	if postgresDB {
		dialector = postgres.New(postgres.Config{Conn: tx})
	} else {
		dialector = sqlite.New(sqlite.Config{Conn: tx})
	}
	gormDB, err := gorm.Open(dialector, &gorm.Config{TranslateError: true})
	if err != nil {
		return nil, fmt.Errorf("open gorm on migration tx: %w", err)
	}
	return gormDB, nil
}

// noopNotifier drops the spaces store's write notifications.
type noopNotifier struct{}

func (noopNotifier) NotifyWrite(
	context.Context, habitat_syntax.SpaceURI, syntax.DID, syntax.TID, []byte,
) {
}

func (noopNotifier) NotifySpaceDeleted(context.Context, habitat_syntax.SpaceURI) {}
