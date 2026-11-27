package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/urfave/cli/v3"

	"github.com/habitat-network/habitat/internal/opensocial"
	habitatsyntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	legacyAccess    = "community.opensocial.access"
	legacyIndex     = "community.opensocial.space"
	spaceRelation   = "network.habitat.relationship.spaceRelation"
	canonicalMeta   = "org.atgrp.meta"
	canonicalRoster = "org.atgrp.members"
)

func groupPreflightCommand() *cli.Command {
	return &cli.Command{
		Name:  "group-preflight",
		Usage: "Inventory legacy group migration blockers without changing data",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "sqlite-snapshot",
				Usage: "Absolute path to a standalone SQLite backup (not a live DB or WAL copy)",
			},
			&cli.BoolFlag{
				Name:  "postgres-snapshot",
				Usage: "Inspect a restored PostgreSQL copy using PEAR_PREFLIGHT_POSTGRES_DSN",
			},
			&cli.StringFlag{
				Name:     "member-domain",
				Usage:    "Member domain configured on the source Pear instance",
				Required: true,
			},
		},
		Action: runGroupPreflight,
	}
}

func runGroupPreflight(ctx context.Context, cmd *cli.Command) error {
	domain := cmd.String("member-domain")
	if _, err := syntax.ParseHandle(domain); err != nil || domain != strings.ToLower(domain) {
		return errors.New("member-domain must be the source instance's lowercase DNS name")
	}
	snapshot := cmd.String("sqlite-snapshot")
	usePostgres := cmd.Bool("postgres-snapshot")
	if (snapshot != "") == usePostgres {
		return errors.New("select exactly one snapshot source")
	}
	driver, dsn := "sqlite3", ""
	opts := sql.TxOptions{ReadOnly: true}
	if usePostgres {
		driver, dsn = "pgx", os.Getenv("PEAR_PREFLIGHT_POSTGRES_DSN")
		if !strings.HasPrefix(dsn, "postgresql://") && !strings.HasPrefix(dsn, "postgres://") {
			return errors.New("set PEAR_PREFLIGHT_POSTGRES_DSN to the restored snapshot URL")
		}
		opts.Isolation = sql.LevelRepeatableRead
	} else {
		dsn = sqlitePreflightDSN(snapshot)
		if dsn == "" {
			return errors.New("sqlite-snapshot must be an absolute path")
		}
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			if _, err := os.Lstat(snapshot + suffix); !errors.Is(err, os.ErrNotExist) {
				return errors.New("SQLite snapshot must be standalone, without database sidecars")
			}
		}
		if info, err := os.Lstat(snapshot); err != nil || !info.Mode().IsRegular() {
			return errors.New("SQLite snapshot must be an existing regular file")
		}
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return errors.New("could not open preflight database")
	}
	defer func() { _ = db.Close() }()
	tx, err := db.BeginTx(ctx, &opts)
	if err != nil {
		return errors.New("could not start read-only preflight transaction")
	}
	defer func() { _ = tx.Rollback() }()
	report, err := inspectLegacyGroups(ctx, tx, domain, usePostgres)
	if err != nil {
		return err
	}
	return json.NewEncoder(cmd.Root().Writer).Encode(report)
}

func sqlitePreflightDSN(path string) string {
	if !filepath.IsAbs(path) {
		return ""
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&immutable=1"}
	return u.String()
}

type preflightCounts struct {
	Groups                 int `json:"candidate_groups"`
	Identities             int `json:"hive_group_handle_candidates"`
	LegacySpaces           int `json:"legacy_group_spaces"`
	DomainMappings         int `json:"email_domain_mappings"`
	LegacyRecords          int `json:"active_legacy_group_records"`
	SpacesWithLegacyAccess int `json:"spaces_with_legacy_access"`
	PendingInvites         int `json:"pending_invites"`
	LegacyShareRecords     int `json:"local_legacy_subject_share_records"`
	InvalidCandidateRows   int `json:"invalid_candidate_rows"`
	BlockedGroups          int `json:"groups_with_blockers"`
	MarkedGroups           int `json:"groups_already_marked"`
}

type preflightReport struct {
	Counts          preflightCounts   `json:"counts"`
	Groups          []*preflightGroup `json:"groups"`
	MarkerAvailable bool              `json:"marker_schema_available"`
	Scope           string            `json:"scope"`
}

type preflightGroup struct {
	DID                 string   `json:"did"`
	Marker              string   `json:"completion_marker"`
	Evidence            []string `json:"evidence"`
	Blockers            []string `json:"blockers"`
	Memberships         int      `json:"memberships"`
	AdminRoleHolders    int      `json:"admin_role_holders"`
	Roles               int      `json:"roles"`
	DomainMappings      int      `json:"email_domain_mappings"`
	CanonicalSpaces     int      `json:"canonical_spaces"`
	ModalitySpaces      int      `json:"other_owned_spaces"`
	LegacyAccessRecords int      `json:"legacy_access_records"`
	LegacyProfiles      int      `json:"legacy_member_profiles"`
	LegacyAcceptances   int      `json:"legacy_acceptances"`
	LegacyAppGrants     int      `json:"legacy_app_grants"`
	LegacyShares        int      `json:"legacy_subject_shares"`
	PendingInvites      int      `json:"pending_invites"`
	NonMemberInvites    int      `json:"pending_non_default_role_invites"`

	hosted             bool
	keysPresent        bool
	kemPresent         bool
	marked             bool
	legacyAbout        bool
	legacyMembers      bool
	validProfile       bool
	permissionsPresent bool
	roleIDs            map[string]bool
	referencedRoles    map[string]bool
}

func (g *preflightGroup) evidence(code string) {
	if !slices.Contains(g.Evidence, code) {
		g.Evidence = append(g.Evidence, code)
	}
}

func (g *preflightGroup) block(code string) {
	if !slices.Contains(g.Blockers, code) {
		g.Blockers = append(g.Blockers, code)
	}
}

type groupInventory struct {
	ctx             context.Context
	tx              *sql.Tx
	domain          string
	markerAvailable bool
	counts          preflightCounts
	groups          map[string]*preflightGroup
}

func (i *groupInventory) group(did string) *preflightGroup {
	if _, err := syntax.ParseDID(did); err != nil {
		i.counts.InvalidCandidateRows++
		return nil
	}
	if g := i.groups[did]; g != nil {
		return g
	}
	g := &preflightGroup{
		DID: did, Evidence: []string{}, Blockers: []string{},
		roleIDs: map[string]bool{}, referencedRoles: map[string]bool{},
	}
	i.groups[did] = g
	return g
}

func inspectLegacyGroups(
	ctx context.Context,
	tx *sql.Tx,
	domain string,
	postgres bool,
) (preflightReport, error) {
	i := groupInventory{ctx: ctx, tx: tx, domain: domain, groups: map[string]*preflightGroup{}}
	if err := i.scanMarkers(postgres); err != nil {
		return preflightReport{}, err
	}
	for _, scan := range []func() error{
		i.scanIdentities, i.scanDomains, i.scanInvites, i.scanSpaces, i.scanRecords, i.scanOtherSpaces, i.scanLegacyAccessCount,
	} {
		if err := scan(); err != nil {
			return preflightReport{}, err
		}
	}
	groups := make([]*preflightGroup, 0, len(i.groups))
	for _, g := range i.groups {
		if g.marked {
			g.Marker = "present"
			g.Blockers = []string{} // Legacy conversion blockers are not applicable.
			i.counts.MarkedGroups++
		} else {
			g.Marker = "schema_unavailable"
			if i.markerAvailable {
				g.Marker = "absent"
			}
			g.finish()
			if len(g.Blockers) > 0 {
				i.counts.BlockedGroups++
			}
			i.counts.Groups++
		}
		sort.Strings(g.Evidence)
		sort.Strings(g.Blockers)
		groups = append(groups, g)
	}
	sort.Slice(groups, func(a, b int) bool { return groups[a].DID < groups[b].DID })
	return preflightReport{
		Counts:          i.counts,
		Groups:          groups,
		MarkerAvailable: i.markerAvailable,
		Scope:           "Inventory only; does not establish readiness, effective-access parity, or authority to cut over.",
	}, nil
}

func (i *groupInventory) scanMarkers(postgres bool) error {
	query := `SELECT COALESCE((SELECT type FROM main.sqlite_schema
		WHERE name = 'canonical_group_rows' AND type IN ('table', 'view')), '')`
	table := "main.canonical_group_rows"
	wantKind := "table"
	if postgres {
		query = `SELECT COALESCE((SELECT relkind::text FROM pg_catalog.pg_class
			WHERE oid = pg_catalog.to_regclass('canonical_group_rows')), '')`
		table = "canonical_group_rows"
		wantKind = "r"
	}
	var kind string
	if err := i.tx.QueryRowContext(i.ctx, query).Scan(&kind); err != nil {
		return errors.New("could not inspect completion marker schema")
	}
	if kind == "" {
		return nil
	}
	if kind != wantKind {
		return errors.New("completion marker schema is not a table")
	}
	i.markerAvailable = true
	return i.scan("completion markers", "SELECT did FROM "+table, func(row *sql.Rows) error {
		var did string
		if err := row.Scan(&did); err != nil {
			return err
		}
		if g := i.group(did); g != nil {
			g.marked = true
			g.evidence("canonical_completion_marker")
		}
		return nil
	})
}

// Keep errors categorical: a driver error can include a DSN or raw data.
func (i *groupInventory) scan(label, query string, visit func(*sql.Rows) error) error {
	rows, err := i.tx.QueryContext(i.ctx, query)
	if err != nil {
		return errors.New("could not query " + label + " on current Pear schema")
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := visit(rows); err != nil {
			return errors.New("could not read " + label)
		}
	}
	if rows.Err() != nil {
		return errors.New("could not finish reading " + label)
	}
	return nil
}

func (i *groupInventory) scanIdentities() error {
	// MintOrgIdentity uses a single-label internal handle. MintIdentity
	// uses a dotted handle for people; neither is inferred to be an admin.
	return i.scan("Hive identity inventory", `SELECT COALESCE(opaque_id, ''),
		COALESCE(signing_public_key, '') <> '' AND COALESCE(signing_private_key_enc, '') <> '',
		COALESCE(kem_public_key, '') <> '' AND COALESCE(kem_private_key_enc, '') <> ''
		FROM idents WHERE handle <> '' AND handle NOT LIKE '%.%'`, func(row *sql.Rows) error {
		var id string
		var keys, kem bool
		if err := row.Scan(&id, &keys, &kem); err != nil {
			return err
		}
		i.counts.Identities++
		if id == "" {
			i.counts.InvalidCandidateRows++
			return nil
		}
		if g := i.group("did:web:" + id + "." + i.domain); g != nil {
			g.evidence("hive_group_handle")
			g.hosted, g.keysPresent, g.kemPresent = true, keys, kem
		}
		return nil
	})
}

func (i *groupInventory) scanSpaces() error {
	return i.scan("legacy space inventory", `SELECT COALESCE(owner, ''), type, COALESCE(skey, '')
		FROM spaces WHERE type IN ('community.opensocial.about', 'community.opensocial.members')`,
		func(row *sql.Rows) error {
			var owner, typ, skey string
			if err := row.Scan(&owner, &typ, &skey); err != nil {
				return err
			}
			i.counts.LegacySpaces++
			if g := i.group(owner); g != nil {
				g.evidence("legacy_group_space")
				if skey != "self" {
					g.block("unexpected_legacy_group_space_key")
				} else if typ == opensocial.AboutSpaceType {
					g.legacyAbout = true
				} else {
					g.legacyMembers = true
				}
			}
			return nil
		})
}

func (i *groupInventory) scanOtherSpaces() error {
	return i.scan("other space counts", `SELECT COALESCE(owner, ''), COALESCE(type, ''), count(*)
		FROM spaces WHERE type NOT IN ('community.opensocial.about', 'community.opensocial.members')
		GROUP BY owner, type`,
		func(row *sql.Rows) error {
			var owner, typ string
			var n int
			if err := row.Scan(&owner, &typ, &n); err != nil {
				return err
			}
			if g := i.groups[owner]; g != nil {
				switch typ {
				case canonicalMeta, canonicalRoster:
					g.CanonicalSpaces += n
				default:
					g.ModalitySpaces += n
				}
			}
			return nil
		})
}

func (i *groupInventory) scanDomains() error {
	return i.scan(
		"email-domain mapping counts",
		`SELECT org_did, count(*) FROM domain_mappings GROUP BY org_did`,
		func(row *sql.Rows) error {
			var did string
			var n int
			if err := row.Scan(&did, &n); err != nil {
				return err
			}
			i.counts.DomainMappings += n
			if g := i.group(did); g != nil {
				g.evidence("email_domain_mapping")
				g.DomainMappings = n
			}
			return nil
		},
	)
}

func (i *groupInventory) scanRecords() error {
	return i.scan("current relationship and group records", `SELECT COALESCE(space_owner, ''),
		COALESCE(space_type, ''), COALESCE(space_skey, ''), COALESCE(repo, ''), collection,
		COALESCE(rkey, ''), value FROM space_records WHERE deleted_at IS NULL
		AND (collection LIKE 'community.opensocial.%' OR collection = 'network.habitat.relationship.spaceRelation'
		OR (space_type = 'community.opensocial.members' AND collection = 'network.habitat.space.appAccess'))`,
		func(row *sql.Rows) error {
			var owner, typ, skey, repo, collection, rkey string
			var raw []byte
			if err := row.Scan(&owner, &typ, &skey, &repo, &collection, &rkey, &raw); err != nil {
				return err
			}
			if collection == spaceRelation {
				i.scanShare(owner, repo, raw)
				return nil
			}
			i.counts.LegacyRecords++
			if g := i.group(owner); g != nil {
				g.evidence("legacy_collection_record")
				g.record(typ, skey, repo, collection, rkey, raw)
			}
			return nil
		})
}

func (i *groupInventory) scanLegacyAccessCount() error {
	if err := i.tx.QueryRowContext(i.ctx, `SELECT count(*) FROM (
		SELECT 1 FROM space_records WHERE deleted_at IS NULL AND collection = 'community.opensocial.access'
		GROUP BY space_owner, space_type, space_skey
	) access_spaces`).Scan(&i.counts.SpacesWithLegacyAccess); err != nil {
		return errors.New("could not count spaces with legacy access records")
	}
	return nil
}

func recordMap(raw []byte) map[string]any {
	value, err := atdata.UnmarshalCBOR(raw)
	if err != nil {
		return nil
	}
	return value
}

func (i *groupInventory) scanShare(owner, repo string, raw []byte) {
	value := recordMap(raw)
	if value == nil {
		i.counts.InvalidCandidateRows++
		if g := i.groups[owner]; g != nil {
			g.block("unreadable_share_record")
		}
		return
	}
	subject, _ := value["subject"].(string)
	uri, err := habitatsyntax.ParseSpaceURI(subject)
	if err != nil || uri.SpaceType() != opensocial.MembersSpaceType {
		return
	}
	did := uri.SpaceOwner().String()
	if i.groups[did] == nil && !strings.HasSuffix(did, "."+i.domain) {
		return // External references aren't hosted migration candidates.
	}
	i.counts.LegacyShareRecords++
	if g := i.group(did); g != nil {
		g.evidence("legacy_share_subject")
		g.LegacyShares++
		if owner != repo {
			g.block("share_not_authored_by_space_owner")
		}
		if uri.Skey() != "self" {
			g.block("unexpected_legacy_share_subject_key")
		}
	}
}

func (g *preflightGroup) record(typ, skey, repo, collection, rkey string, raw []byte) {
	memberSpace := typ == opensocial.MembersSpaceType && skey == "self"
	switch collection {
	case opensocial.ProfileCollection:
		value := recordMap(raw)
		name, _ := value["name"].(string)
		if typ != opensocial.AboutSpaceType || skey != "self" || repo != g.DID || rkey != "self" ||
			strings.TrimSpace(name) == "" {
			g.block("invalid_legacy_profile_record")
		} else {
			g.validProfile = true
		}
	case opensocial.RoleCollection, opensocial.MembershipCollection:
		if !memberSpace || repo != g.DID {
			g.block("unexpected_roster_record_author_or_location")
			return
		}
		if collection == opensocial.RoleCollection {
			g.Roles++
			value := recordMap(raw)
			name, _ := value["name"].(string)
			if _, err := syntax.ParseRecordKey(rkey); err != nil || strings.TrimSpace(name) == "" {
				g.block("invalid_role_record")
				return
			}
			g.roleIDs[rkey] = true
			return
		}
		g.Memberships++
		value := recordMap(raw)
		roles, ok := recordRoles(value)
		if _, err := syntax.ParseDID(rkey); err != nil || !ok {
			g.block("invalid_membership_record")
			return
		}
		for _, role := range roles {
			g.referencedRoles[role] = true
		}
		if slices.Contains(roles, opensocial.AdminRoleRkey) {
			g.AdminRoleHolders++
		}
	case opensocial.AcceptanceCollection:
		g.LegacyAcceptances++
	case opensocial.MemberProfileCollection:
		g.LegacyProfiles++
	case string(habitatsyntax.AppAccessCollection):
		g.LegacyAppGrants++
	case legacyAccess:
		g.LegacyAccessRecords++
		roles, ok := recordRoles(recordMap(raw))
		if repo != g.DID || rkey != "self" || !ok {
			g.block("invalid_legacy_access_record")
			return
		}
		for _, role := range roles {
			g.referencedRoles[role] = true
		}
	case opensocial.PermissionsCollection:
		value := recordMap(raw)
		if !memberSpace || repo != g.DID || rkey != "self" || value == nil {
			g.block("invalid_legacy_configuration_record")
			return
		}
		g.permissionsPresent = true
		g.permissions(value)
	case legacyIndex:
		value := recordMap(raw)
		uri, _ := value["uri"].(string)
		target, err := habitatsyntax.ParseSpaceURI(uri)
		if !memberSpace || repo != g.DID || err != nil || target.SpaceOwner().String() != g.DID {
			g.block("invalid_legacy_configuration_record")
		}
	default:
		g.block("unmapped_legacy_collection")
	}
}

func recordRoles(value map[string]any) ([]string, bool) {
	raw, ok := value["roles"].([]any)
	if !ok {
		return nil, false
	}
	roles := make([]string, 0, len(raw))
	for _, entry := range raw {
		role, ok := entry.(string)
		if !ok || role == "" {
			return nil, false
		}
		roles = append(roles, role)
	}
	return roles, true
}

func (g *preflightGroup) permissions(value map[string]any) {
	bindings, bindingsOK := value["bindings"].([]any)
	assignable, assignableOK := value["assignable"].([]any)
	if !bindingsOK || !assignableOK {
		g.block("invalid_legacy_configuration_record")
	}
	for _, raw := range bindings {
		binding, _ := raw.(map[string]any)
		roles, ok := recordRoles(binding)
		if !ok {
			g.block("invalid_legacy_configuration_record")
			continue
		}
		for _, role := range roles {
			g.referencedRoles[role] = true
		}
		action, _ := binding["action"].(string)
		switch opensocial.Action(action) {
		case opensocial.ActionCommunityConfigure:
			if len(roles) > 0 {
				g.block("legacy_role_editors_need_permission_mapping")
			}
		case opensocial.ActionMcpConfigure:
			if len(roles) > 0 {
				g.block("legacy_host_capabilities_need_permission_mapping")
			}
		case opensocial.ActionInvite, opensocial.ActionEject, opensocial.ActionRoleAssign,
			opensocial.ActionSpaceCreate, opensocial.ActionSpaceConfigure, opensocial.ActionSpaceDelete:
		default:
			g.block("unmapped_legacy_permission_action")
		}
	}
	for _, raw := range assignable {
		binding, _ := raw.(map[string]any)
		role, roleOK := binding["role"].(string)
		roles, rolesOK := recordRoles(binding)
		if !roleOK || role == "" || !rolesOK {
			g.block("invalid_legacy_configuration_record")
			continue
		}
		g.referencedRoles[role] = true
		for _, target := range roles {
			if target != "*" {
				g.referencedRoles[target] = true
			}
		}
	}
}

func (i *groupInventory) scanInvites() error {
	return i.scan("pending invite counts and intent", `SELECT org_id, roles FROM invite_rows`,
		func(row *sql.Rows) error {
			var did string
			var raw []byte
			if err := row.Scan(&did, &raw); err != nil {
				return err
			}
			i.counts.PendingInvites++
			if g := i.group(did); g != nil {
				g.evidence("pending_legacy_invite")
				g.PendingInvites++
				var roles []string
				if json.Unmarshal(raw, &roles) != nil || len(roles) != 1 ||
					roles[0] != opensocial.MemberRoleRkey {
					g.NonMemberInvites++
					g.block("pending_invite_requires_authorized_reissue")
				}
			}
			return nil
		})
}

func (g *preflightGroup) finish() {
	if !g.hosted || !g.keysPresent {
		g.block("hosted_identity_or_signing_keys_missing")
	}
	if g.hosted && !g.kemPresent {
		g.block("hive_kem_keys_missing")
	}
	if !g.legacyAbout || !g.legacyMembers {
		g.block("legacy_self_spaces_incomplete")
	}
	if !g.validProfile {
		g.block("legacy_profile_missing_or_invalid")
	}
	if g.AdminRoleHolders == 0 || !g.roleIDs[opensocial.AdminRoleRkey] {
		g.block("no_group_authored_legacy_admin_membership")
	}
	if !g.permissionsPresent {
		g.block("legacy_implicit_admin_permissions_need_mapping")
	}
	for role := range g.referencedRoles {
		if !g.roleIDs[role] {
			g.block("undeclared_legacy_role")
		}
	}
	if g.CanonicalSpaces > 0 {
		g.block("canonical_spaces_need_marker_and_conflict_review")
	}
	if g.LegacyShares > 0 {
		g.block("legacy_share_provenance_needs_reconciliation")
	}
	if g.LegacyAppGrants > 0 {
		g.block("legacy_app_grants_need_reconciliation")
	}
}
