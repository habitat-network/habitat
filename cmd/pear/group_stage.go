package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/urfave/cli/v3"

	"github.com/habitat-network/habitat/internal/opensocial"
	habitatsyntax "github.com/habitat-network/habitat/internal/syntax"
)

func groupStageCommand() *cli.Command {
	return &cli.Command{
		Name:  "group-stage",
		Usage: "Prepare a private, revision-pinned conversion plan; never change group access",
		Flags: append(
			groupSnapshotFlags(),
			&cli.StringFlag{Name: "group-did", Required: true, Usage: "Existing group DID"},
			&cli.StringFlag{
				Name:     "plan-file",
				Required: true,
				Usage:    "Absolute plan path in a private directory",
			},
		),
		Action: runGroupStage,
	}
}

func runGroupStage(ctx context.Context, cmd *cli.Command) error {
	did, err := syntax.ParseDID(cmd.String("group-did"))
	if err != nil {
		return errors.New("group-did must be a valid DID")
	}
	var plan *groupStagePlan
	err = withGroupSnapshot(ctx, cmd, func(tx *sql.Tx, domain string, postgres bool) error {
		var readErr error
		plan, readErr = buildGroupStagePlan(ctx, tx, did.String(), domain, postgres)
		return readErr
	})
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return errors.New("could not encode the private conversion plan")
	}
	if err := publishGroupStagePlan(cmd.String("plan-file"), append(body, '\n')); err != nil {
		return err
	}
	_, err = fmt.Fprintln(
		cmd.Root().Writer,
		"Private conversion plan written or verified; no group state changed.",
	)
	return err
}

type stageSpaceRef struct {
	Owner string `json:"owner"`
	Type  string `json:"type"`
	Key   string `json:"skey"`
}

type stageSpace struct {
	stageSpaceRef
	Revision string `json:"revision"`
	Pin      string `json:"pin"`
	Use      string `json:"intent"`
}

type stageLocator struct {
	Space      stageSpaceRef `json:"space"`
	Repo       string        `json:"repo"`
	Collection string        `json:"collection"`
	Key        string        `json:"rkey"`
}

type stageRecord struct {
	stageLocator
	Revision string        `json:"revision"`
	CID      string        `json:"cid"`
	Created  string        `json:"source_created_at,omitempty"`
	Pin      string        `json:"pin"`
	Deleted  bool          `json:"deleted"`
	Intent   string        `json:"intent"`
	Target   *stageLocator `json:"candidate_target,omitempty"`
	Subject  string        `json:"original_share_subject,omitempty"`
	Roles    *[]string     `json:"roles,omitempty"`
}

type stageInvite struct {
	ID     string    `json:"source_id"`
	Pin    string    `json:"pin"`
	Roles  *[]string `json:"roles,omitempty"`
	Intent string    `json:"intent"`
}

type groupStagePlan struct {
	Version      int             `json:"version"`
	DID          string          `json:"did"`
	MemberDomain string          `json:"source_member_domain"`
	Marker       string          `json:"completion_marker"`
	SourcePin    string          `json:"source_pin,omitempty"`
	Handle       string          `json:"handle,omitempty"`
	IdentityPin  string          `json:"identity_pin,omitempty"`
	JoinPolicy   string          `json:"required_join_policy"`
	TargetSpaces []stageSpaceRef `json:"candidate_target_spaces"`
	Spaces       []stageSpace    `json:"source_spaces"`
	Records      []stageRecord   `json:"source_records"`
	Invites      []stageInvite   `json:"pending_invite_intent"`
	OtherPins    []string        `json:"other_source_pins"`
	Blockers     []string        `json:"blockers"`
}

type groupStageReader struct {
	*groupInventory
	plan     *groupStagePlan
	g        *preflightGroup
	param    string
	evidence bool
}

func buildGroupStagePlan(
	ctx context.Context,
	tx *sql.Tx,
	did, domain string,
	postgres bool,
) (*groupStagePlan, error) {
	i := &groupInventory{ctx: ctx, tx: tx, domain: domain, groups: map[string]*preflightGroup{}}
	r := &groupStageReader{
		groupInventory: i, g: i.group(did), param: "?1",
		plan: &groupStagePlan{
			Version:      1,
			DID:          did,
			MemberDomain: domain,
			JoinPolicy:   "invite",
			Marker:       "schema_unavailable",
			TargetSpaces: []stageSpaceRef{
				{did, canonicalMeta, "self"},
				{did, canonicalRoster, "self"},
			},
			Spaces:    []stageSpace{},
			Records:   []stageRecord{},
			Invites:   []stageInvite{},
			OtherPins: []string{},
		},
	}
	if r.g == nil {
		return nil, errors.New("group-did must be a valid DID")
	}
	if postgres {
		r.param = "$1"
	}
	table, err := groupMarkerTable(ctx, tx, postgres)
	if err != nil {
		return nil, err
	}
	if table != "" {
		var present bool
		query := "SELECT EXISTS (SELECT 1 FROM " + table + " WHERE did = " + r.param + ")"
		if err := tx.QueryRowContext(ctx, query, did).Scan(&present); err != nil {
			return nil, errors.New("could not look up group completion marker")
		}
		if present {
			return nil, errors.New("group already has a canonical completion marker")
		}
		r.plan.Marker = "absent"
	} else {
		r.g.block("completion_marker_schema_unavailable")
	}
	for _, scan := range []func() error{
		r.identity, r.domains, r.spaces, r.invites, r.records, r.additionalPins,
	} {
		if err := scan(); err != nil {
			return nil, err
		}
	}
	if !r.evidence {
		return nil, errors.New("no legacy group evidence for group-did on this snapshot")
	}
	r.g.finish()
	r.g.block("effective_access_requires_fenced_verification")
	sort.Strings(r.g.Blockers)
	r.plan.Blockers = r.g.Blockers
	sort.Strings(r.plan.OtherPins)
	r.plan.SourcePin, err = stagePin(r.plan)
	return r.plan, err
}

// Fingerprint the authoritative values as well as revision fields. Older rows
// may not have revisions, and not all changes (e.g. policies or invites)
// advance a space revision. Never serialize key material into the plan.
func stagePin(values ...any) (string, error) {
	data, err := json.Marshal(values)
	if err != nil {
		return "", errors.New("could not fingerprint group snapshot")
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (r *groupStageReader) identity() error {
	prefix, suffix := "did:web:", "."+r.domain
	if !strings.HasPrefix(r.plan.DID, prefix) || !strings.HasSuffix(r.plan.DID, suffix) {
		return nil
	}
	opaque := strings.TrimSuffix(strings.TrimPrefix(r.plan.DID, prefix), suffix)
	return r.scan(
		"group Hive identity",
		`SELECT handle, signing_public_key, signing_private_key_enc,
		kem_public_key, kem_private_key_enc FROM idents WHERE opaque_id = `+r.param,
		func(row *sql.Rows) error {
			var handle, pub, priv, kemPub, kemPriv sql.NullString
			if err := row.Scan(&handle, &pub, &priv, &kemPub, &kemPriv); err != nil {
				return err
			}
			if handle.String == "" || strings.Contains(handle.String, ".") {
				return nil // Never select a personal Hive identity.
			}
			r.evidence, r.g.hosted = true, true
			r.g.keysPresent = pub.String != "" && priv.String != ""
			r.g.kemPresent = kemPub.String != "" && kemPriv.String != ""
			r.plan.Handle = handle.String + "." + r.domain
			var err error
			r.plan.IdentityPin, err = stagePin(opaque, handle, pub, priv, kemPub, kemPriv)
			return err
		},
		opaque,
	)
}

func (r *groupStageReader) domains() error {
	return r.scan(
		"group domain mappings",
		`SELECT domain, login_method FROM domain_mappings WHERE org_did = `+
			r.param,
		func(row *sql.Rows) error {
			var domain, method sql.NullString
			if err := row.Scan(&domain, &method); err != nil {
				return err
			}
			r.evidence = true
			r.g.block("email_auto_enrollment_requires_cutover_review")
			return r.addPin("domain", domain, method)
		},
		r.plan.DID,
	)
}

func (r *groupStageReader) spaces() error {
	return r.scan("group spaces", `SELECT COALESCE(type, ''), COALESCE(skey, ''), COALESCE(rev, ''),
		policy FROM spaces WHERE owner = `+r.param+` ORDER BY type, skey`, func(row *sql.Rows) error {
		s := stageSpace{
			stageSpaceRef: stageSpaceRef{Owner: r.plan.DID},
			Use:           "preserve_address_in_place",
		}
		var policy []byte
		if err := row.Scan(&s.Type, &s.Key, &s.Revision, &policy); err != nil {
			return err
		}
		switch s.Type {
		case opensocial.AboutSpaceType, opensocial.MembersSpaceType:
			r.evidence = true
			s.Use = "retain_legacy_source"
			if s.Key != "self" {
				r.g.block("unexpected_legacy_group_space_key")
			} else if s.Type == opensocial.AboutSpaceType {
				r.g.legacyAbout = true
			} else {
				r.g.legacyMembers = true
			}
		default:
			if strings.HasPrefix(s.Type, "org.atgrp.") {
				s.Use = "occupied_canonical_namespace"
				r.g.block("canonical_target_conflict")
			} else {
				r.g.block("modality_policy_requires_access_review")
			}
		}
		_, typeErr := syntax.ParseNSID(s.Type)
		_, keyErr := habitatsyntax.ParseSkey(s.Key)
		if typeErr != nil || keyErr != nil {
			r.g.block("invalid_source_space_address")
		}
		pin, err := stagePin(s.stageSpaceRef, s.Revision, policy)
		if err != nil {
			return err
		}
		s.Pin = pin
		r.plan.Spaces = append(r.plan.Spaces, s)
		return nil
	}, r.plan.DID)
}

func (r *groupStageReader) invites() error {
	return r.scan("group invite intent", `SELECT COALESCE(id, ''), invitee, roles, created_at
		FROM invite_rows WHERE org_id = `+r.param+` ORDER BY id`, func(row *sql.Rows) error {
		invite := stageInvite{Intent: "retain_source_and_verify_exact_reissue"}
		var invitee, rawRoles string
		var created time.Time
		if err := row.Scan(&invite.ID, &invitee, &rawRoles, &created); err != nil {
			return err
		}
		r.evidence = true
		r.g.block("pending_invite_requires_authorized_reissue")
		roles, validRoles := decodeStageInviteRoles(rawRoles)
		_, didErr := syntax.ParseDID(invitee)
		switch {
		case !validRoles || invite.ID == "" || didErr != nil:
			invite.Intent = "manual_invite_recovery"
			r.g.block("malformed_pending_invite")
		default:
			invite.Roles = &roles
			if len(roles) != 1 || roles[0] != opensocial.MemberRoleRkey {
				invite.Intent = "retain_exact_source_roles_for_recovery"
			}
		}
		var err error
		invite.Pin, err = stagePin(invite.ID, invitee, rawRoles, created.UTC())
		r.plan.Invites = append(r.plan.Invites, invite)
		return err
	}, r.plan.DID)
}

func decodeStageInviteRoles(raw string) ([]string, bool) {
	var source []*string
	if err := json.Unmarshal([]byte(raw), &source); err != nil {
		return nil, false
	}
	if source == nil {
		return nil, true // Preserve the difference between null and [].
	}
	roles := make([]string, len(source))
	for i, role := range source {
		if role == nil {
			return nil, false
		}
		roles[i] = *role
	}
	return roles, true
}

func (r *groupStageReader) records() error {
	return r.scan("group records and relationship sources", `SELECT COALESCE(space_owner, ''),
		COALESCE(space_type, ''), COALESCE(space_skey, ''), COALESCE(repo, ''), collection,
		COALESCE(rkey, ''), COALESCE(rev, ''), COALESCE(cid, ''), COALESCE(prev_cid, ''), created_at, deleted_at, value
		FROM space_records WHERE collection = 'network.habitat.relationship.spaceRelation'
		OR (space_owner = `+r.param+` AND (space_type IN ('community.opensocial.about', 'community.opensocial.members')
		OR space_type LIKE 'org.atgrp.%' OR collection LIKE 'org.atgrp.%'
		OR collection LIKE 'community.opensocial.%'
		OR collection IN ('network.habitat.relationship.userRelation', 'network.habitat.space.appAccess')))
		ORDER BY space_owner, space_type, space_skey, repo, collection, rkey`, r.record, r.plan.DID)
}

func (r *groupStageReader) record(row *sql.Rows) error {
	rec := stageRecord{Intent: "retain_legacy_source"}
	var raw []byte
	var created, deleted sql.NullTime
	var prev string
	s := &rec.Space
	if err := row.Scan(&s.Owner, &s.Type, &s.Key, &rec.Repo, &rec.Collection, &rec.Key,
		&rec.Revision, &rec.CID, &prev, &created, &deleted, &raw); err != nil {
		return err
	}
	rec.Deleted = deleted.Valid
	if created.Valid {
		rec.Created = created.Time.UTC().Format(time.RFC3339Nano)
	}
	value := recordMap(raw)
	if rec.Collection == spaceRelation {
		subject, _ := value["subject"].(string)
		uri, err := habitatsyntax.ParseSpaceURI(subject)
		if err != nil {
			// An invalid subject cannot safely be ruled out as incoming.
			r.g.block("unattributed_space_relationship_requires_review")
		}
		if s.Owner != r.plan.DID && (err != nil || uri.SpaceOwner().String() != r.plan.DID) {
			return nil
		}
		rec.Subject = subject
		rec.Intent = "reconcile_share_with_original_subject"
		r.g.block("legacy_share_provenance_needs_reconciliation")
		if !rec.Deleted && uri.SpaceType() == opensocial.MembersSpaceType &&
			uri.SpaceOwner().String() == r.plan.DID {
			r.evidence = true
		}
	} else if !rec.Deleted {
		r.classify(&rec, value, raw)
	}
	if s.Owner == r.plan.DID &&
		(strings.HasPrefix(s.Type, "org.atgrp.") || strings.HasPrefix(rec.Collection, "org.atgrp.")) {
		r.g.block("canonical_target_conflict")
		rec.Intent = "occupied_canonical_namespace"
		rec.Target = nil
	}
	pin, err := stagePin(
		rec.stageLocator,
		rec.Revision,
		rec.CID,
		prev,
		rec.Created,
		deleted.Time.UTC(),
		deleted.Valid,
		raw,
	)
	if err != nil {
		return err
	}
	rec.Pin = pin
	r.plan.Records = append(r.plan.Records, rec)
	return nil
}

var canonicalStageRoleKey = regexp.MustCompile(`^[a-z-]{3,}$`)

func (r *groupStageReader) classify(rec *stageRecord, value map[string]any, raw []byte) {
	s := rec.Space
	validRoster := s.Type == opensocial.MembersSpaceType && s.Key == "self" &&
		rec.Repo == r.plan.DID
	target := func(space, coll, key string) {
		rec.Target = &stageLocator{Space: stageSpaceRef{r.plan.DID, space, "self"},
			Repo: r.plan.DID, Collection: coll, Key: key}
	}
	if strings.HasPrefix(rec.Collection, "community.opensocial.") {
		r.evidence = true
	}
	if strings.HasPrefix(rec.Collection, "community.opensocial.") ||
		s.Type == opensocial.AboutSpaceType || s.Type == opensocial.MembersSpaceType {
		r.g.record(s.Type, s.Key, rec.Repo, rec.Collection, rec.Key, raw)
	}
	if roles, ok := recordRoles(
		value,
	); ok &&
		(rec.Collection == legacyAccess || rec.Collection == opensocial.MembershipCollection) {
		rec.Roles = &roles
	}
	switch rec.Collection {
	case opensocial.ProfileCollection:
		if name, _ := value["name"].(string); s.Type == opensocial.AboutSpaceType &&
			s.Key == "self" && rec.Repo == r.plan.DID && rec.Key == "self" && strings.TrimSpace(name) != "" {
			rec.Intent = "project_profile_from_pinned_source"
			target(canonicalMeta, "org.atgrp.about", "self")
		}
		if value["avatar"] != nil {
			r.g.block("avatar_attribution_requires_review")
		}
	case opensocial.RoleCollection:
		rec.Intent = "retain_role_id_and_resolve_exact_grants"
		r.g.block("role_grants_require_explicit_mapping")
		if !canonicalStageRoleKey.MatchString(rec.Key) {
			r.g.block("legacy_role_id_requires_mapping")
		} else if name, _ := value["name"].(string); validRoster && strings.TrimSpace(name) != "" {
			target(canonicalRoster, "org.atgrp.role", rec.Key)
		}
	case opensocial.MembershipCollection:
		rec.Intent = "retain_membership_and_exact_roles"
		if rec.Created == "" {
			r.g.block("membership_creation_time_requires_review")
		}
		if _, err := syntax.ParseDID(rec.Key); validRoster && err == nil && rec.Roles != nil {
			target(canonicalRoster, "org.atgrp.membership", rec.Key)
		}
	case opensocial.PermissionsCollection:
		rec.Intent = "resolve_permission_bindings_and_assignability_from_source"
		r.g.block("role_grants_require_explicit_mapping")
	case opensocial.AcceptanceCollection, opensocial.MemberProfileCollection:
		rec.Intent = "retain_source_never_synthesize_personal_confirmation"
		r.g.block("personal_confirmation_provenance_requires_review")
	case legacyAccess, legacyIndex:
		rec.Intent = "reconcile_in_place_without_changing_space_address"
		r.g.block("modality_policy_requires_access_review")
	case string(habitatsyntax.AppAccessCollection):
		rec.Intent = "preserve_app_grant_intent"
		r.g.block("legacy_app_grants_need_reconciliation")
	case string(habitatsyntax.UserRelationCollection):
		rec.Intent = "reconcile_direct_share"
		r.g.block("legacy_share_provenance_needs_reconciliation")
	}
}

func (r *groupStageReader) additionalPins() error {
	if err := r.scan(
		"group space repo heads",
		`SELECT space_type, space_skey, repo, rev, space_rev, hash,
		COALESCE(remote, false), COALESCE(removed, false), deleted_at FROM space_repos WHERE space_owner = `+
			r.param,
		func(row *sql.Rows) error {
			var typ, key, repo, rev, spaceRev sql.NullString
			var hash []byte
			var remote, removed bool
			var deleted sql.NullTime
			if err := row.Scan(
				&typ,
				&key,
				&repo,
				&rev,
				&spaceRev,
				&hash,
				&remote,
				&removed,
				&deleted,
			); err != nil {
				return err
			}
			if remote {
				r.g.block("remote_repo_requires_access_review")
			}
			if strings.HasPrefix(typ.String, "org.atgrp.") {
				r.g.block("canonical_target_conflict")
			}
			return r.addPin(
				"repo",
				typ,
				key,
				repo,
				rev,
				spaceRev,
				hash,
				remote,
				removed,
				deleted.Time.UTC(),
				deleted.Valid,
			)
		},
		r.plan.DID,
	); err != nil {
		return err
	}
	if err := r.scan(
		"group space tombstones",
		`SELECT type, skey, deleted_at FROM deleted_spaces WHERE owner = `+
			r.param,
		func(row *sql.Rows) error {
			var typ, key sql.NullString
			var deleted sql.NullTime
			if err := row.Scan(&typ, &key, &deleted); err != nil {
				return err
			}
			if strings.HasPrefix(typ.String, "org.atgrp.") {
				r.g.block("canonical_target_conflict")
			}
			return r.addPin("deleted_space", typ, key, deleted.Time.UTC(), deleted.Valid)
		},
		r.plan.DID,
	); err != nil {
		return err
	}
	return r.scan(
		"canonical remote record conflicts",
		`SELECT space_type, space_skey, repo, collection,
		rkey, rev, cid, deleted_at, value FROM space_remote_records WHERE space_owner = `+r.param+
			` AND (space_type LIKE 'org.atgrp.%' OR collection LIKE 'org.atgrp.%')`,
		func(row *sql.Rows) error {
			var typ, key, repo, collection, rkey, rev, cid sql.NullString
			var deleted sql.NullTime
			var raw []byte
			if err := row.Scan(
				&typ,
				&key,
				&repo,
				&collection,
				&rkey,
				&rev,
				&cid,
				&deleted,
				&raw,
			); err != nil {
				return err
			}
			r.g.block("canonical_target_conflict")
			return r.addPin(
				"remote_record",
				typ,
				key,
				repo,
				collection,
				rkey,
				rev,
				cid,
				deleted.Time.UTC(),
				deleted.Valid,
				raw,
			)
		},
		r.plan.DID,
	)
}

func (r *groupStageReader) addPin(kind string, fields ...any) error {
	pin, err := stagePin(fields...)
	if err == nil {
		r.plan.OtherPins = append(r.plan.OtherPins, kind+":"+pin)
	}
	return err
}

// Publish only a private local artifact. A hard link publishes complete bytes
// without replacing a previous attempt. Retries compare the existing plan
// including its interpretation and source pin; different bytes fail closed.
func publishGroupStagePlan(path string, body []byte) error {
	if !filepath.IsAbs(path) {
		return errors.New("plan-file must be an absolute path")
	}
	dir, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return errors.New("could not open private plan directory")
	}
	defer func() { _ = dir.Close() }()
	if info, err := dir.Stat("."); err != nil || info.Mode().Perm() != 0o700 {
		return errors.New("plan-file must be in a private directory with mode 0700")
	}
	tempName := ".group-stage-" + rand.Text()
	tmp, err := dir.OpenFile(tempName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.New("could not create private plan artifact")
	}
	defer func() { _ = dir.Remove(tempName) }()
	_, writeErr := tmp.Write(body)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		return errors.New("could not write private plan artifact")
	}
	name := filepath.Base(path)
	if err := dir.Link(tempName, name); errors.Is(err, os.ErrExist) {
		return verifyGroupStagePlan(dir, name, body)
	} else if err != nil {
		return errors.New("could not publish private plan artifact")
	}
	return nil
}

func verifyGroupStagePlan(dir *os.Root, name string, body []byte) error {
	info, err := dir.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errors.New("existing plan is not a private regular file")
	}
	f, err := dir.Open(name)
	if err != nil {
		return errors.New("could not open existing plan")
	}
	defer func() { _ = f.Close() }()
	current, err := f.Stat()
	if err != nil || !os.SameFile(info, current) {
		return errors.New("existing plan changed during verification")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(len(body))+1))
	if err != nil || !bytes.Equal(data, body) {
		return errors.New("existing plan conflicts with this snapshot; it was not replaced")
	}
	return nil
}
