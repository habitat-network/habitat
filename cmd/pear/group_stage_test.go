package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitatsyntax "github.com/habitat-network/habitat/internal/syntax"
)

func stageSnapshot(t *testing.T, db *gorm.DB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snapshot.db")
	require.NoError(t, db.Exec("VACUUM INTO ?", path).Error)
	return path
}

func stageArtifactDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "private")
	require.NoError(t, os.Mkdir(dir, 0o700))
	return dir
}

func runStage(t *testing.T, did, path string, flags ...string) (string, error) {
	t.Helper()
	var stdout bytes.Buffer
	app := &cli.Command{Writer: &stdout, Commands: commands()}
	args := []string{
		"pear",
		"run",
		"group-stage",
		"--member-domain",
		"example.com",
		"--group-did",
		did,
		"--plan-file",
		path,
	}
	err := app.Run(t.Context(), append(args, flags...))
	return stdout.String(), err
}

func readStage(t *testing.T, path string) ([]byte, groupStagePlan) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var plan groupStagePlan
	require.NoError(t, json.Unmarshal(data, &plan))
	return data, plan
}

func TestGroupStageSQLiteSnapshot(t *testing.T) {
	ctx := t.Context()
	db := pear_testutil.NewPearDB(t)
	direct, _, _, _ := seedLegacyPreflight(t, db)
	did := syntax.DID(direct)
	store := opensocial_testutil.NewTestStore(t, opensocial_testutil.WithDB(db))
	legacy := habitatsyntax.ConstructSpaceURI(did, opensocial.MembersSpaceType, "self")
	modality := habitatsyntax.ConstructSpaceURI(did, "network.habitat.docs", "doc")
	foreign := syntax.DID("did:plc:foreign-share-owner")
	foreignSpace, err := store.SpaceStore.CreateSpace(
		ctx,
		foreign,
		"network.habitat.docs",
		"shared",
	)
	require.NoError(t, err)
	_, _, err = store.SpaceStore.PutRecord(ctx, foreignSpace, foreign, spaceRelation, "incoming",
		spaces_testutil.MustMarshalRecord(t, map[string]any{
			"$type": spaceRelation, "subject": modality.AtSpace().String(),
			"subjectRole": "reader", "relation": "reader",
		}))
	require.NoError(t, err)
	snapshot := stageSnapshot(t, db)
	sourceBytes, err := os.ReadFile(snapshot)
	require.NoError(t, err)

	path := filepath.Join(stageArtifactDir(t), "attempt.json")
	output, err := runStage(t, direct, path, "--sqlite-snapshot", snapshot)
	require.NoError(t, err)
	require.NotContains(t, output, direct)
	require.NotContains(t, output, "custom-editor")
	first, plan := readStage(t, path)
	stat, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), stat.Mode().Perm())

	require.Equal(t, direct, plan.DID)
	require.Equal(t, "private-direct-label.example.com", plan.Handle)
	require.Len(t, plan.SourcePin, 64)
	require.Len(t, plan.IdentityPin, 64)
	require.Equal(t, "invite", plan.JoinPolicy)
	require.Equal(t, "schema_unavailable", plan.Marker)
	require.Equal(
		t,
		[]stageSpaceRef{{direct, canonicalMeta, "self"}, {direct, canonicalRoster, "self"}},
		plan.TargetSpaces,
	)
	require.Contains(t, plan.Blockers, "role_grants_require_explicit_mapping")
	require.Contains(t, plan.Blockers, "legacy_app_grants_need_reconciliation")
	require.Contains(t, plan.Blockers, "effective_access_requires_fenced_verification")
	require.Contains(t, plan.Blockers, "pending_invite_requires_authorized_reissue")
	require.NotContains(t, plan.Blockers, "canonical_target_conflict")
	require.True(t, slices.ContainsFunc(plan.Spaces, func(s stageSpace) bool {
		return s.stageSpaceRef == (stageSpaceRef{direct, "network.habitat.docs", "doc"}) &&
			s.Revision != "" && s.Use == "preserve_address_in_place"
	}))

	recordByKey := map[string]stageRecord{}
	for _, record := range plan.Records {
		require.Len(t, record.Pin, 64)
		recordByKey[record.Key] = record
	}
	require.Equal(t, legacy.Legacy().String(), recordByKey["share"].Subject)
	require.Equal(t, modality.AtSpace().String(), recordByKey["incoming"].Subject)
	require.Equal(t, foreign.String(), recordByKey["incoming"].Space.Owner)
	require.True(t, recordByKey["removed-share"].Deleted)
	require.Equal(t, legacy.AtSpace().String(), recordByKey["removed-share"].Subject)
	membership := recordByKey[fixtureMember]
	require.NotEmpty(t, membership.Created)
	require.NotNil(t, membership.Target)
	require.Equal(t, direct, membership.Target.Repo)
	require.Equal(t, "org.atgrp.membership", membership.Target.Collection)
	require.ElementsMatch(t, []string{"member", "admin"}, *membership.Roles)
	require.Equal(t, "resolve_permission_bindings_and_assignability_from_source",
		findStageRecord(t, plan, opensocial.PermissionsCollection).Intent)
	require.Len(t, plan.Invites, 2)
	require.True(t, slices.ContainsFunc(plan.Invites, func(invite stageInvite) bool {
		return slices.Equal(*invite.Roles, []string{"custom-editor"}) &&
			invite.Intent == "retain_exact_source_roles_for_recovery"
	}))
	require.NotContains(t, string(first), "private-invitee")
	require.NotContains(t, string(first), "private-app.example")

	_, err = runStage(t, direct, path, "--sqlite-snapshot", snapshot)
	require.NoError(t, err)
	replay, _ := readStage(t, path)
	require.Equal(t, first, replay)
	retryStat, err := os.Stat(path)
	require.NoError(t, err)
	require.True(t, os.SameFile(stat, retryStat), "retry must not replace the prior attempt")

	// An unaffected group's mutation must not change this group's plan.
	other, err := store.NewOrg(ctx, "unrelated", "did:plc:unrelated-admin")
	require.NoError(t, err)
	secondSnapshot := stageSnapshot(t, db)
	_, err = runStage(t, direct, path, "--sqlite-snapshot", secondSnapshot)
	require.NoError(t, err)
	otherPath := filepath.Join(filepath.Dir(path), "other.json")
	_, err = runStage(t, other, otherPath, "--sqlite-snapshot", secondSnapshot)
	require.NoError(t, err)
	otherBytes, _ := readStage(t, otherPath)

	// Discarding this attempt deletes only its private local artifact.
	require.NoError(t, os.Remove(path))
	otherAfter, _ := readStage(t, otherPath)
	require.Equal(t, otherBytes, otherAfter)
	// Use legacy readers on the very snapshot that was planned.
	restored, err := gorm.Open(sqlite.Open(sqlitePreflightDSN(snapshot)))
	require.NoError(t, err)
	conn, err := restored.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	legacyStore := opensocial_testutil.NewTestStore(t, opensocial_testutil.WithDB(restored))
	allowed, err := legacyStore.CheckAction(
		ctx,
		did,
		fixtureMember,
		opensocial.ActionCommunityConfigure,
	)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = legacyStore.CheckAction(
		ctx,
		did,
		"did:plc:nonmember",
		opensocial.ActionCommunityConfigure,
	)
	require.NoError(t, err)
	require.False(t, allowed)
	policy, err := legacyStore.SpaceStore.GetSpacePolicy(ctx, modality)
	require.NoError(t, err)
	require.Equal(t, spaces.PolicyRelationSpace, policy.Type)
	invites, err := legacyStore.ListPendingInvites(ctx, did)
	require.NoError(t, err)
	require.Len(t, invites, 2)
	exists, err := legacyStore.SpaceStore.CheckSpaceExists(
		ctx,
		habitatsyntax.ConstructSpaceURI(did, canonicalRoster, "self"),
	)
	require.NoError(t, err)
	require.False(t, exists)
	after, err := os.ReadFile(snapshot)
	require.NoError(t, err)
	require.Equal(t, sourceBytes, after)
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		require.NoFileExists(t, snapshot+suffix)
	}
}

func findStageRecord(t *testing.T, plan groupStagePlan, collection string) stageRecord {
	t.Helper()
	for _, record := range plan.Records {
		if record.Collection == collection {
			return record
		}
	}
	t.Fatalf("missing record collection %s", collection)
	return stageRecord{}
}

func TestGroupStageRefusesStaleAndConflictingPlans(t *testing.T) {
	ctx := t.Context()
	db := pear_testutil.NewPearDB(t)
	direct, _, _, partial := seedLegacyPreflight(t, db)
	did := syntax.DID(direct)
	first := stageSnapshot(t, db)
	path := filepath.Join(stageArtifactDir(t), "attempt.json")
	_, err := runStage(t, direct, path, "--sqlite-snapshot", first)
	require.NoError(t, err)
	before, baseline := readStage(t, path)

	// Policy changes don't advance spaces.rev. A plan must still detect them.
	store := opensocial_testutil.NewTestStore(t, opensocial_testutil.WithDB(db))
	modality := habitatsyntax.ConstructSpaceURI(did, "network.habitat.docs", "doc")
	require.NoError(t, store.SpaceStore.SetSpacePolicy(ctx, modality,
		spaces.Policy{Type: spaces.PolicyMemberSpace, PublicRead: true}))
	changed := stageSnapshot(t, db)
	output, err := runStage(t, direct, path, "--sqlite-snapshot", changed)
	require.ErrorContains(t, err, "conflicts with this snapshot")
	require.Empty(t, output)
	current, _ := readStage(t, path)
	require.Equal(t, before, current)
	changePath := filepath.Join(filepath.Dir(path), "policy-changed.json")
	_, err = runStage(t, direct, changePath, "--sqlite-snapshot", changed)
	require.NoError(t, err)
	_, changePlan := readStage(t, changePath)
	require.NotEqual(t, baseline.SourcePin, changePlan.SourcePin)
	require.Equal(t, baseline.Spaces[2].Revision, changePlan.Spaces[2].Revision)

	require.NoError(t, db.Exec("UPDATE invite_rows SET roles = ? WHERE org_id = ?",
		`["member","custom-editor"]`, direct).Error)
	_, err = runStage(t, direct, changePath, "--sqlite-snapshot", stageSnapshot(t, db))
	require.ErrorContains(t, err, "conflicts with this snapshot")
	invitePath := filepath.Join(filepath.Dir(path), "invites-changed.json")
	_, err = runStage(t, direct, invitePath, "--sqlite-snapshot", stageSnapshot(t, db))
	require.NoError(t, err)
	_, invitePlan := readStage(t, invitePath)
	for _, invite := range invitePlan.Invites {
		require.Equal(t, []string{"member", "custom-editor"}, *invite.Roles)
	}

	// The exact permissions row is pinned even if its revision and CID are stale.
	customPermissions := spaces_testutil.MustMarshalRecord(t, map[string]any{
		"$type": opensocial.PermissionsCollection,
		"bindings": []any{
			map[string]any{"action": "community.configure", "roles": []string{"custom-editor"}},
		},
		"assignable": []any{
			map[string]any{"role": "admin", "roles": []string{"member", "custom-editor"}},
		},
	})
	require.NoError(
		t,
		db.Exec(`UPDATE space_records SET value = ? WHERE space_owner = ? AND collection = ?`,
			customPermissions, direct, opensocial.PermissionsCollection).Error,
	)
	_, err = runStage(t, direct, invitePath, "--sqlite-snapshot", stageSnapshot(t, db))
	require.ErrorContains(t, err, "conflicts with this snapshot")

	_, _, err = store.SpaceStore.PutRecord(
		ctx,
		modality,
		did,
		"org.atgrp.about",
		"self",
		spaces_testutil.MustMarshalRecord(
			t,
			map[string]any{"$type": "org.atgrp.about", "name": "Occupied"},
		),
	)
	require.NoError(t, err)
	conflict := filepath.Join(filepath.Dir(path), "conflict.json")
	_, err = runStage(t, direct, conflict, "--sqlite-snapshot", stageSnapshot(t, db))
	require.NoError(t, err)
	_, occupied := readStage(t, conflict)
	require.Contains(t, occupied.Blockers, "canonical_target_conflict")
	require.Equal(
		t,
		"occupied_canonical_namespace",
		findStageRecord(t, occupied, "org.atgrp.about").Intent,
	)

	partialPath := filepath.Join(filepath.Dir(path), "partial.json")
	_, err = runStage(t, partial, partialPath, "--sqlite-snapshot", first)
	require.NoError(t, err)
	_, orphan := readStage(t, partialPath)
	require.Contains(t, orphan.Blockers, "canonical_target_conflict")
	require.Contains(t, orphan.Blockers, "no_group_authored_legacy_admin_membership")

	require.NoError(
		t,
		db.Exec(`CREATE TABLE canonical_group_rows (did text NOT NULL PRIMARY KEY)`).Error,
	)
	require.NoError(t, db.Exec(`INSERT INTO canonical_group_rows (did) VALUES (?)`, direct).Error)
	refused := filepath.Join(filepath.Dir(path), "already-marked.json")
	_, err = runStage(t, direct, refused, "--sqlite-snapshot", stageSnapshot(t, db))
	require.ErrorContains(t, err, "already has a canonical completion marker")
	require.NoFileExists(t, refused)
	_, err = runStage(t, "did:web:unhosted.example.com", refused, "--sqlite-snapshot", first)
	require.ErrorContains(t, err, "no legacy group evidence")
	require.NoFileExists(t, refused)
}

func TestGroupStageRejectsInsecureAndOccupiedArtifactPaths(t *testing.T) {
	dir := stageArtifactDir(t)
	path := filepath.Join(dir, "artifact")
	require.NoError(t, os.WriteFile(path, []byte("existing"), 0o600))
	require.ErrorContains(t, publishGroupStagePlan(path, []byte("different")), "conflicts")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "existing", string(data))
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(path, link))
	require.ErrorContains(t, publishGroupStagePlan(link, data), "not a private regular file")
	public := filepath.Join(dir, "public")
	require.NoError(t, os.Mkdir(public, 0o755))
	require.ErrorContains(
		t,
		publishGroupStagePlan(filepath.Join(public, "artifact"), data),
		"mode 0700",
	)
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 3, "failed attempts must clean up only their own temporary files")
}

func TestGroupStagePostgresSnapshot(t *testing.T) {
	db := newKeyvalPostgres(t)
	direct, email, _, _ := seedLegacyPreflight(t, db)
	t.Setenv("PEAR_PREFLIGHT_POSTGRES_DSN", db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(
		t,
		db.Exec(`CREATE TABLE canonical_group_rows (did text NOT NULL PRIMARY KEY)`).Error,
	)
	path := filepath.Join(stageArtifactDir(t), "attempt.json")
	_, err := runStage(t, direct, path, "--postgres-snapshot")
	require.NoError(t, err)
	first, plan := readStage(t, path)
	require.Equal(t, "absent", plan.Marker)
	require.Len(t, plan.Invites, 2)
	_, err = runStage(t, direct, path, "--postgres-snapshot")
	require.NoError(t, err)
	repeated, _ := readStage(t, path)
	require.Equal(t, first, repeated)
	emailPath := filepath.Join(filepath.Dir(path), "email.json")
	_, err = runStage(t, email, emailPath, "--postgres-snapshot")
	require.NoError(t, err)
	_, emailPlan := readStage(t, emailPath)
	require.Contains(t, emailPlan.Blockers, "email_auto_enrollment_requires_cutover_review")
	require.Contains(t, emailPlan.Blockers, "personal_confirmation_provenance_requires_review")
}
