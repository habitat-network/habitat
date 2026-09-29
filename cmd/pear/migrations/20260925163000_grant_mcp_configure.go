package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/pressly/goose/v3"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/opensocial"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func init() {
	goose.AddMigrationContext(upGrantMcpConfigure, downGrantMcpConfigure)
}

// upGrantMcpConfigure binds the mcp.configure action to the admin role in the
// community.opensocial.permissions record of every existing opensocial org
// that doesn't already bind it, matching the default NewOrg seeds for new orgs.
// Orgs without a permissions record are left alone: CheckAction already
// authorizes their admins for every action, and writing a record here would
// replace that fallback with just the one binding.
//
// The records are rewritten through the opensocial store from the
// PearMigrationContext, so the members space's repo hash and rev stay
// consistent and registered syncers are notified of the write.
func upGrantMcpConfigure(ctx context.Context, tx *sql.Tx) error {
	mc, err := GetPearMigrationContext(ctx, tx)
	if err != nil {
		return err
	}

	// Read every org before writing any, so the query's rows are closed before
	// the store's statements run on the same transaction.
	var spaceRepos []struct{ Space, Repo string }
	if err := mc.DB.WithContext(ctx).
		Raw(
			"SELECT space, repo FROM space_records "+
				"WHERE collection = ? AND rkey = 'self' AND deleted_at IS NULL",
			opensocial.PermissionsCollection,
		).
		Scan(&spaceRepos).Error; err != nil {
		return fmt.Errorf("list permissions records: %w", err)
	}
	var orgs []syntax.DID
	for _, sr := range spaceRepos {
		uri, err := habitat_syntax.ParseSpaceURI(sr.Space)
		if err != nil {
			return fmt.Errorf("parse space %q: %w", sr.Space, err)
		}
		// Only an org's own members space holds its authz configuration.
		if uri.SpaceType() != opensocial.MembersSpaceType || uri.Skey() != "self" ||
			uri.SpaceOwner().String() != sr.Repo {
			continue
		}
		orgs = append(orgs, uri.SpaceOwner())
	}
	if len(orgs) == 0 {
		return nil
	}
	if mc.Opensocial == nil {
		return errors.New("no opensocial store in the pear migration context")
	}

	for _, org := range orgs {
		permissions, err := mc.Opensocial.GetPermissions(ctx, org)
		if err != nil {
			return fmt.Errorf("get permissions of %s: %w", org, err)
		}
		if slices.ContainsFunc(permissions.Bindings,
			func(b opensocial_api.CommunityOpensocialPermissionsActionBinding) bool {
				return opensocial.Action(b.Action) == opensocial.ActionMcpConfigure
			}) {
			continue
		}
		permissions.Bindings = append(permissions.Bindings,
			opensocial_api.CommunityOpensocialPermissionsActionBinding{
				Action: string(opensocial.ActionMcpConfigure),
				Roles:  []string{opensocial.AdminRoleRkey},
			})
		if err := mc.Opensocial.PutPermissions(
			ctx, org, permissions.Bindings, permissions.Assignable,
		); err != nil {
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
