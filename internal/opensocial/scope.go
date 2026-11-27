package opensocial

import (
	"net/url"
	"slices"
	"strings"
)

// Scopes are the OAuth-style grants the opensocial spec gives roles. A scope
// is one of:
//
//	include:<nsid>                  a permission set, e.g. include:org.atgrp.configure
//	space:<type>?authority=&skey=&collection=&action=&manage=
//	                                 writes (action) or management (manage) of spaces
//	rpc:<lxm>?aud=                  a group method call
//	read, create, update, delete    in a space's access record: that action in that space
//	habitat:<name>=<value>          Habitat-only grants with no spec form, e.g. habitat:mcp=configure
//
// A set of held scopes covers a wanted scope when every permission the wanted
// scope stands for is contained in some held permission, after permission sets
// are expanded. There are no denies: a member holds the union of their roles'
// scopes.

const (
	includePrefix = "include:"
	spacePrefix   = "space:"
	rpcPrefix     = "rpc:"
	wildcard      = "*"
)

// permission is one expanded scope.
type permission struct {
	kind        string // "space", "rpc" or "literal"
	spaceType   string
	authority   string
	skey        string
	collections []string
	actions     []string
	manage      []string
	lxm         string
	aud         string
	literal     string
}

func parseQuery(rest string) (string, url.Values) {
	name, query, _ := strings.Cut(rest, "?")
	values, err := url.ParseQuery(query)
	if err != nil {
		values = url.Values{}
	}
	return name, values
}

// expand turns scope into the permissions it stands for.
func expand(scope string) []permission {
	switch {
	case strings.HasPrefix(scope, HabitatActionScopePrefix):
		// A Habitat action stands for the scopes it requires.
		var out []permission
		for _, required := range ActionScopes(
			Action(strings.TrimPrefix(scope, HabitatActionScopePrefix)),
		) {
			out = append(out, expand(required)...)
		}
		return out
	case strings.HasPrefix(scope, includePrefix):
		return permissionSets[strings.TrimPrefix(scope, includePrefix)]
	case strings.HasPrefix(scope, spacePrefix):
		spaceType, q := parseQuery(strings.TrimPrefix(scope, spacePrefix))
		return []permission{{
			kind:        "space",
			spaceType:   spaceType,
			authority:   q.Get("authority"),
			skey:        q.Get("skey"),
			collections: q["collection"],
			actions:     q["action"],
			manage:      q["manage"],
		}}
	case strings.HasPrefix(scope, rpcPrefix):
		lxm, q := parseQuery(strings.TrimPrefix(scope, rpcPrefix))
		return []permission{{kind: "rpc", lxm: lxm, aud: q.Get("aud")}}
	default:
		return []permission{{kind: "literal", literal: scope}}
	}
}

// containsAll reports whether held, a list where empty means unrestricted, is a
// superset of want. An empty want against a restricted held is not contained.
func containsAll(held, want []string) bool {
	if len(held) == 0 {
		return true
	}
	if len(want) == 0 {
		return false
	}
	for _, w := range want {
		if !slices.Contains(held, w) {
			return false
		}
	}
	return true
}

func matches(held, want string) bool {
	return held == "" || held == wildcard || held == want
}

// covers reports whether the held permission contains want.
func (held permission) covers(want permission) bool {
	if held.kind != want.kind {
		return false
	}
	switch held.kind {
	case "space":
		return matches(held.spaceType, want.spaceType) &&
			matches(held.authority, want.authority) &&
			matches(held.skey, want.skey) &&
			containsAll(held.collections, want.collections) &&
			containsAll(held.actions, want.actions) &&
			(len(want.manage) == 0 || containsAll(held.manage, want.manage))
	case "rpc":
		return held.lxm == want.lxm && matches(held.aud, want.aud)
	default:
		return held.literal == want.literal
	}
}

// ScopeSet is the union of the scopes a member holds.
type ScopeSet struct {
	// all is true for the group DID itself, which holds every scope.
	all    bool
	scopes []string
}

// AllScopes is the scope set of the group DID, which holds every scope.
func AllScopes() ScopeSet { return ScopeSet{all: true} }

// NewScopeSet returns the union of scopes.
func NewScopeSet(scopes ...[]string) ScopeSet {
	return ScopeSet{scopes: slices.Concat(scopes...)}
}

// Covers reports whether the set grants scope.
func (s ScopeSet) Covers(scope string) bool {
	if s.all {
		return true
	}
	var held []permission
	for _, h := range s.scopes {
		held = append(held, expand(h)...)
	}
	wants := expand(scope)
	if len(wants) == 0 {
		return false
	}
	for _, want := range wants {
		if !slices.ContainsFunc(held, func(h permission) bool { return h.covers(want) }) {
			return false
		}
	}
	return true
}

// permissionSets mirrors the org.atgrp permission-set lexicons
// (authManageBasic, authManageMembers, authModerate, authManageAdmin,
// authParticipate).
var permissionSets = map[string][]permission{
	"org.atgrp.authManageBasic": {
		{
			kind: "space", spaceType: MetaSpaceType, authority: "self", skey: "self",
			collections: []string{AboutCollection}, actions: allActions,
		},
		{
			kind: "space", spaceType: MembersSpaceType, authority: "self", skey: "self",
			collections: []string{RuleCollection}, actions: allActions,
		},
	},
	"org.atgrp.authManageMembers": manageMembersPermissions,
	"org.atgrp.authModerate":      moderatePermissions,
	"org.atgrp.authManageAdmin": slices.Concat(
		[]permission{
			{
				kind: "space", spaceType: MetaSpaceType, authority: "self", skey: "self",
				collections: []string{AboutCollection, ConfigCollection, LabelCollection},
				actions:     allActions, manage: []string{"update"},
			},
			{
				kind: "space", spaceType: MembersSpaceType, authority: "self", skey: "self",
				collections: []string{
					RuleCollection, LabelerCollection, RoleCollection, MembershipCollection,
					LabelCollection,
				},
				actions: allActions, manage: []string{"update"},
			},
		},
		manageMembersPermissions[1:],
		moderatePermissions[len(moderatePermissions)-3:],
	),
	"org.atgrp.authParticipate": {
		{
			kind: "space", spaceType: MetaSpaceType, authority: wildcard, skey: "self",
			actions: []string{"read"},
		},
		{
			kind: "space", spaceType: MembersSpaceType, authority: wildcard, skey: "self",
			collections: []string{ConfirmationCollection}, actions: allActions,
		},
		{
			kind: "space", spaceType: InboxSpaceType, authority: "self", skey: "self",
			actions: []string{"read"}, manage: []string{"create"},
		},
		{kind: "rpc", lxm: "org.atgrp.requestJoin", aud: wildcard},
		{kind: "rpc", lxm: "org.atgrp.cancelJoinRequest", aud: wildcard},
		{kind: "rpc", lxm: "org.atgrp.leaveGroup", aud: wildcard},
		{kind: "rpc", lxm: "org.atgrp.mod.submitReport", aud: wildcard},
		{kind: "rpc", lxm: "org.atgrp.mod.submitAppeal", aud: wildcard},
		{kind: "rpc", lxm: "org.atgrp.mod.querySelfHistory", aud: wildcard},
	},
}

var manageMembersPermissions = []permission{
	{
		kind: "space", spaceType: MembersSpaceType, authority: "self", skey: "self",
		collections: []string{MembershipCollection}, actions: allActions,
	},
	{
		kind: "space", spaceType: InboxSpaceType, authority: wildcard, skey: "self",
		collections: []string{InviteCollection},
		actions:     []string{"read_self", "create", "update", "delete"},
	},
	{kind: "rpc", lxm: "org.atgrp.listJoinRequests", aud: wildcard},
	{kind: "rpc", lxm: "org.atgrp.denyJoinRequest", aud: wildcard},
}

var moderatePermissions = []permission{
	{
		kind: "space", spaceType: MetaSpaceType, authority: "self", skey: "self",
		collections: []string{LabelCollection}, actions: allActions,
	},
	{
		kind: "space", spaceType: MembersSpaceType, authority: "self", skey: "self",
		collections: []string{LabelCollection}, actions: allActions,
	},
	{
		kind: "space", spaceType: MembersSpaceType, authority: "self", skey: "self",
		collections: []string{MembershipCollection}, actions: []string{"read", "delete"},
	},
	{kind: "rpc", lxm: "org.atgrp.mod.querySubjects", aud: wildcard},
	{kind: "rpc", lxm: "org.atgrp.mod.querySubjectHistory", aud: wildcard},
	{kind: "rpc", lxm: "org.atgrp.mod.updateSubject", aud: wildcard},
}

var allActions = []string{"read", "create", "update", "delete"}
