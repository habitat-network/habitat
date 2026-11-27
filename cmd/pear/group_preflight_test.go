package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitatsyntax "github.com/habitat-network/habitat/internal/syntax"
)

const fixtureMember = "did:plc:synthetic-member"

func seedLegacyPreflight(t *testing.T, db *gorm.DB) (direct, email, adminless, partial string) {
	t.Helper()
	ctx := t.Context()
	store := opensocial_testutil.NewTestStore(t, opensocial_testutil.WithDB(db))
	emailStore, err := emaildomain.NewStore(db)
	require.NoError(t, err)
	direct, err = store.NewOrg(ctx, "private-direct-label", fixtureMember)
	require.NoError(t, err)
	email, err = store.NewOrgWithoutCreator(ctx, "private-email-label")
	require.NoError(t, err)
	require.NoError(
		t,
		emailStore.CreateDomainMapping(
			ctx,
			"private.example",
			syntax.DID(email),
			emaildomain.LoginMethodGoogle,
		),
	)
	require.NoError(t, store.ProvisionMember(ctx, syntax.DID(email), fixtureMember))
	adminless, err = store.NewOrgWithoutCreator(ctx, "never-signed-in")
	require.NoError(t, err)
	require.NoError(
		t,
		emailStore.CreateDomainMapping(
			ctx,
			"never-signed-in.example",
			syntax.DID(adminless),
			emaildomain.LoginMethodGoogle,
		),
	)

	did := syntax.DID(direct)
	members := habitatsyntax.ConstructSpaceURI(did, opensocial.MembersSpaceType, "self")
	_, _, err = store.SpaceStore.PutRecord(
		ctx,
		members,
		did,
		opensocial.RoleCollection,
		"custom-editor",
		spaces_testutil.MustMarshalRecord(
			t,
			map[string]any{"$type": opensocial.RoleCollection, "name": "Custom editor"},
		),
	)
	require.NoError(t, err)
	_, err = store.CreateInvite(ctx, did, "did:plc:private-invitee", []string{"custom-editor"})
	require.NoError(t, err)
	_, err = store.CreateInvite(ctx, did, "did:plc:private-member-invitee", []string{"member"})
	require.NoError(t, err)
	require.NoError(
		t,
		store.GrantAppAccess(
			ctx,
			did,
			"https://private-app.example/client.json",
			[]string{"transition:generic"},
		),
	)
	modality, err := store.CreateSpace(
		ctx,
		did,
		[]string{"custom-editor"},
		"network.habitat.docs",
		"doc",
		spaces.Policy{Type: spaces.PolicyRelationSpace},
	)
	require.NoError(t, err)
	_, _, err = store.SpaceStore.PutRecord(ctx, modality, did, spaceRelation, "share",
		spaces_testutil.MustMarshalRecord(t, map[string]any{
			"$type": spaceRelation, "subject": members.Legacy().String(),
			"subjectRole": "reader", "relation": "reader",
		}))
	require.NoError(t, err)
	_, _, err = store.SpaceStore.PutRecord(ctx, modality, did, spaceRelation, "removed-share",
		spaces_testutil.MustMarshalRecord(t, map[string]any{
			"$type": spaceRelation, "subject": members.AtSpace().String(),
			"subjectRole": "reader", "relation": "reader",
		}))
	require.NoError(t, err)
	require.NoError(
		t,
		store.SpaceStore.DeleteRecord(ctx, modality, did, spaceRelation, "removed-share"),
	)

	// Simulate a partial legacy shell and a forged/non-owner membership.
	ident, err := store.Hive.MintOrgIdentity(ctx, "incomplete")
	require.NoError(t, err)
	partial = ident.DID.String()
	partialSpace, err := store.SpaceStore.CreateSpace(
		ctx,
		ident.DID,
		opensocial.MembersSpaceType,
		"self",
	)
	require.NoError(t, err)
	_, _, err = store.SpaceStore.PutRecord(
		ctx,
		partialSpace,
		fixtureMember,
		opensocial.MembershipCollection,
		fixtureMember,
		spaces_testutil.MustMarshalRecord(t, map[string]any{
			"$type":     opensocial.MembershipCollection,
			"roles":     []string{"admin"},
			"updatedAt": "2026-01-01T00:00:00Z",
		}),
	)
	require.NoError(t, err)
	_, err = store.SpaceStore.CreateSpace(ctx, ident.DID, canonicalRoster, "self")
	require.NoError(t, err)
	_, err = store.Hive.MintIdentity(ctx, "person", "private-email-label")
	require.NoError(t, err)
	return direct, email, adminless, partial
}

func assertLegacyPreflight(
	t *testing.T,
	report preflightReport,
	direct, email, adminless, partial string,
) {
	t.Helper()
	require.Contains(t, report.Scope, "does not establish readiness")
	require.Equal(t, 4, report.Counts.Groups)
	require.Equal(t, 4, report.Counts.Identities)
	require.Equal(t, 7, report.Counts.LegacySpaces)
	require.Equal(t, 2, report.Counts.DomainMappings)
	require.Equal(t, 2, report.Counts.PendingInvites)
	require.Equal(t, 1, report.Counts.LegacyShareRecords)
	require.Equal(t, 7, report.Counts.SpacesWithLegacyAccess)
	require.Equal(t, 26, report.Counts.LegacyRecords)
	require.Equal(t, 4, report.Counts.BlockedGroups)
	byDID := map[string]*preflightGroup{}
	for _, group := range report.Groups {
		byDID[group.DID] = group
	}
	require.NotNil(t, byDID[direct])
	d := byDID[direct]
	require.Equal(t, 1, d.AdminRoleHolders)
	require.Equal(t, 3, d.Roles)
	require.Equal(t, 1, d.ModalitySpaces)
	require.Equal(t, 1, d.LegacyShares)
	require.Equal(t, 2, d.PendingInvites)
	require.Equal(t, 1, d.NonMemberInvites)
	require.Contains(t, d.Blockers, "pending_invite_requires_authorized_reissue")
	require.Contains(t, d.Blockers, "legacy_share_provenance_needs_reconciliation")
	require.Contains(t, d.Blockers, "legacy_role_editors_need_permission_mapping")
	require.Contains(t, d.Blockers, "legacy_host_capabilities_need_permission_mapping")
	require.NotContains(t, d.Blockers, "undeclared_legacy_role")
	require.NotContains(t, d.Blockers, "invalid_legacy_configuration_record")
	require.Equal(t, 1, byDID[email].AdminRoleHolders)
	require.Equal(t, 1, byDID[email].LegacyAcceptances)
	require.Equal(t, 1, byDID[email].DomainMappings)
	require.Equal(t, 0, byDID[adminless].Memberships)
	require.Contains(t, byDID[adminless].Blockers, "no_group_authored_legacy_admin_membership")
	require.Equal(t, 0, byDID[partial].AdminRoleHolders)
	require.Equal(t, 1, byDID[partial].CanonicalSpaces)
	require.Contains(t, byDID[partial].Blockers, "legacy_self_spaces_incomplete")
	require.Contains(t, byDID[partial].Blockers, "no_group_authored_legacy_admin_membership")
	require.Contains(t, byDID[partial].Blockers, "unexpected_roster_record_author_or_location")
	require.Equal(t, 1, d.LegacyAppGrants)
	require.Contains(t, d.Blockers, "legacy_app_grants_need_reconciliation")
}

func TestGroupPreflightSQLiteSnapshot(t *testing.T) {
	ctx := t.Context()
	db := pear_testutil.NewPearDB(t)
	direct, email, adminless, partial := seedLegacyPreflight(t, db)
	snapshot := filepath.Join(t.TempDir(), "standalone backup.db")
	require.NoError(t, db.Exec("VACUUM INTO ?", snapshot).Error)
	before, err := os.ReadFile(snapshot)
	require.NoError(t, err)
	report := func() []byte {
		t.Helper()
		var output bytes.Buffer
		app := &cli.Command{Writer: &output, Commands: commands()}
		require.NoError(t, app.Run(ctx, []string{
			"pear",
			"run",
			"group-preflight",
			"--member-domain",
			"example.com",
			"--sqlite-snapshot",
			snapshot,
		}))
		return output.Bytes()
	}
	first := report()
	require.Equal(t, first, report(), "repeated snapshot reads must yield the same inventory")
	after, err := os.ReadFile(snapshot)
	require.NoError(t, err)
	require.Equal(t, before, after)
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		require.NoFileExists(t, snapshot+suffix)
	}
	readonly, err := sql.Open("sqlite3", sqlitePreflightDSN(snapshot))
	require.NoError(t, err)
	t.Cleanup(func() { _ = readonly.Close() })
	_, err = readonly.ExecContext(ctx, "DELETE FROM spaces")
	require.Error(t, err)

	var result preflightReport
	require.NoError(t, json.Unmarshal(first, &result))
	assertLegacyPreflight(t, result, direct, email, adminless, partial)
	require.False(t, result.MarkerAvailable)
	for _, group := range result.Groups {
		require.Equal(t, "schema_unavailable", group.Marker)
	}
	for _, secret := range []string{fixtureMember, "private-direct-label", "private-email-label", "private.example",
		"private-invitee", "custom-editor", "private-app.example", "signing_private_key_enc"} {
		require.NotContains(t, string(first), secret)
	}
}

func TestGroupPreflightMarkerAndOrphanEvidence(t *testing.T) {
	ctx := t.Context()
	db := pear_testutil.NewPearDB(t)
	direct, _, _, partial := seedLegacyPreflight(t, db)
	// Mirror the optional marker schema introduced by the canonical bootstrap.
	require.NoError(
		t,
		db.Exec(`CREATE TABLE canonical_group_rows (did text NOT NULL PRIMARY KEY)`).Error,
	)
	require.NoError(
		t,
		db.Exec(
			`INSERT INTO canonical_group_rows (did) VALUES (?), (?)`,
			direct,
			"did:web:new-group.example.com",
		).Error,
	)
	require.NoError(t, db.Exec(`INSERT INTO idents (handle) VALUES (?)`, "unattributable").Error)
	require.NoError(
		t,
		db.Exec(
			`UPDATE idents SET kem_private_key_enc = NULL WHERE handle = ?`,
			"incomplete",
		).Error,
	)
	emailStore, err := emaildomain.NewStore(db)
	require.NoError(t, err)
	orphan := syntax.DID("did:web:orphan.example.com")
	require.NoError(
		t,
		emailStore.CreateDomainMapping(
			ctx,
			"orphan.example",
			orphan,
			emaildomain.LoginMethodGoogle,
		),
	)

	snapshot := filepath.Join(t.TempDir(), "snapshot.db")
	require.NoError(t, db.Exec("VACUUM INTO ?", snapshot).Error)
	var output bytes.Buffer
	app := &cli.Command{Writer: &output, Commands: commands()}
	require.NoError(t, app.Run(ctx, []string{
		"pear",
		"run",
		"group-preflight",
		"--member-domain",
		"example.com",
		"--sqlite-snapshot",
		snapshot,
	}))
	var result preflightReport
	require.NoError(t, json.Unmarshal(output.Bytes(), &result))
	require.True(t, result.MarkerAvailable)
	require.Equal(t, 4, result.Counts.Groups)
	require.Equal(t, 2, result.Counts.MarkedGroups)
	require.Equal(t, 1, result.Counts.InvalidCandidateRows)
	byDID := map[string]*preflightGroup{}
	for _, group := range result.Groups {
		byDID[group.DID] = group
	}
	require.Equal(t, "present", byDID[direct].Marker)
	require.Equal(t, 2, byDID[direct].PendingInvites, "marker does not hide legacy inventory")
	require.Empty(t, byDID[direct].Blockers)
	require.Equal(t, "present", byDID["did:web:new-group.example.com"].Marker)
	require.Equal(t, "absent", byDID[partial].Marker, "staged roster is not a completion marker")
	require.Contains(t, byDID[partial].Blockers, "canonical_spaces_need_marker_and_conflict_review")
	require.Contains(t, byDID[partial].Blockers, "hive_kem_keys_missing")
	require.Equal(t, []string{"email_domain_mapping"}, byDID[orphan.String()].Evidence)
	require.Contains(t, byDID[orphan.String()].Blockers, "hosted_identity_or_signing_keys_missing")
	require.NotContains(t, output.String(), "unattributable")
}

func TestGroupPreflightPostgresSnapshot(t *testing.T) {
	ctx := t.Context()
	db := newKeyvalPostgres(t)
	direct, email, adminless, partial := seedLegacyPreflight(t, db)
	// Use the same entry point as an operator, against a synthetic test DB.
	t.Setenv("PEAR_PREFLIGHT_POSTGRES_DSN", db.Dialector.(*postgres.Dialector).DSN)
	var output bytes.Buffer
	app := &cli.Command{Writer: &output, Commands: commands()}
	require.NoError(t, app.Run(ctx, []string{
		"pear", "run", "group-preflight", "--member-domain", "example.com", "--postgres-snapshot",
	}))
	var result preflightReport
	require.NoError(t, json.Unmarshal(output.Bytes(), &result))
	assertLegacyPreflight(t, result, direct, email, adminless, partial)
	require.False(t, result.MarkerAvailable)
	require.NoError(
		t,
		db.Exec(`CREATE TABLE canonical_group_rows (did text NOT NULL PRIMARY KEY)`).Error,
	)
	require.NoError(t, db.Exec(`INSERT INTO canonical_group_rows (did) VALUES (?)`, direct).Error)
	output.Reset()
	app = &cli.Command{Writer: &output, Commands: commands()}
	require.NoError(t, app.Run(ctx, []string{
		"pear", "run", "group-preflight", "--member-domain", "example.com", "--postgres-snapshot",
	}))
	require.NoError(t, json.Unmarshal(output.Bytes(), &result))
	require.True(t, result.MarkerAvailable)
	require.Equal(t, 1, result.Counts.MarkedGroups)
	require.Equal(t, 3, result.Counts.Groups)
}

func TestGroupPreflightRejectsUnreadableMarkerSchema(t *testing.T) {
	ctx := t.Context()
	db := pear_testutil.NewPearDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE canonical_group_rows (unexpected_column text)`).Error)
	snapshot := filepath.Join(t.TempDir(), "snapshot.db")
	require.NoError(t, db.Exec("VACUUM INTO ?", snapshot).Error)
	var output bytes.Buffer
	app := &cli.Command{Writer: &output, Commands: commands()}
	err := app.Run(ctx, []string{
		"pear",
		"run",
		"group-preflight",
		"--member-domain",
		"example.com",
		"--sqlite-snapshot",
		snapshot,
	})
	require.ErrorContains(t, err, "could not query completion markers")
	require.Empty(t, output.String())
}

func TestGroupPreflightRequiresExplicitSnapshot(t *testing.T) {
	app := &cli.Command{Commands: commands()}
	err := app.Run(
		t.Context(),
		[]string{"pear", "run", "group-preflight", "--member-domain", "example.com"},
	)
	require.ErrorContains(t, err, "select exactly one snapshot source")

	app = &cli.Command{Commands: commands()}
	path := filepath.Join(t.TempDir(), "live.db")
	require.NoError(t, os.WriteFile(path+"-wal", nil, 0o600))
	err = app.Run(t.Context(), []string{
		"pear",
		"run",
		"group-preflight",
		"--member-domain",
		"example.com",
		"--sqlite-snapshot",
		path,
	})
	require.ErrorContains(t, err, "must be standalone")
}
