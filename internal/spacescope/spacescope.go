// Package spacescope parses and matches the space: OAuth scopes of atproto
// permissioned data (proposal 0016, "OAuth scopes"):
//
//	space:<spaceType>[?authority=<did>][&skey=<skey>][&collection=<nsid>...][&action=<action>...][&manage=<op>...]
//
// A grant selects spaces by (authority, spaceType, skey) and says what it
// permits on their records (action, collection) and on the spaces themselves
// (manage). Only the read side is matched here so far: which spaces, and
// whose records in them, a token may read.
package spacescope

import (
	"fmt"
	"slices"
	"strings"

	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/syntax"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Action is a record operation a grant permits.
type Action string

const (
	ActionReadSelf Action = "read_self"
	ActionRead     Action = "read"
	ActionCreate   Action = "create"
	ActionUpdate   Action = "update"
	ActionDelete   Action = "delete"
)

// allActions is what a grant without any action parameter permits.
var allActions = []Action{ActionRead, ActionCreate, ActionUpdate, ActionDelete}

// wildcard matches any value of a selector.
const wildcard = "*"

// Grant is one parsed space: scope.
type Grant struct {
	// SpaceType is a space type NSID, or "*" for any.
	SpaceType string
	// Authority is the space authority DID, or "*" for any. The scope's
	// "self" (and its default) is resolved to the granting user.
	Authority string
	// Skey is a space key, or "*" for any.
	Skey string
	// Collections are the collections writes are allowed to; reads ignore
	// them. Empty means the space type's declared collections.
	Collections []string
	Actions     []Action
	Manage      []string
}

// Parse parses scope as a space: grant made by self. ok is false, with no
// error, for a scope that isn't a space: scope at all. Syntax is checked by
// indigo's [auth.ParsePermissionString].
func Parse(scope string, self syntax.DID) (grant Grant, ok bool, err error) {
	if !strings.HasPrefix(scope, "space:") {
		return Grant{}, false, nil
	}
	perm, err := auth.ParsePermissionString(scope)
	if err != nil {
		return Grant{}, false, fmt.Errorf("space scope %q: %w", scope, err)
	}
	grant = Grant{
		SpaceType:   perm.SpaceType,
		Authority:   self.String(),
		Skey:        wildcard,
		Collections: perm.Collection,
		Manage:      perm.Manage,
	}
	if perm.Authority != "" && perm.Authority != "self" {
		grant.Authority = perm.Authority
	}
	if perm.SpaceKey != "" {
		grant.Skey = perm.SpaceKey
	}
	for _, raw := range perm.Action {
		grant.Actions = append(grant.Actions, Action(raw))
	}
	if len(grant.Actions) == 0 {
		grant.Actions = allActions
	}
	return grant, true, nil
}

// Covers reports whether the grant selects space.
func (g Grant) Covers(space habitat_syntax.SpaceURI) bool {
	return matches(g.Authority, space.SpaceOwner().String()) &&
		matches(g.SpaceType, space.SpaceType().String()) &&
		matches(g.Skey, string(space.Skey()))
}

func matches(selector, value string) bool {
	return selector == wildcard || selector == value
}

// Read is how much of a space a token may read.
type Read int

const (
	// ReadNone: nothing.
	ReadNone Read = iota
	// ReadSelf: only the granting user's own repo.
	ReadSelf
	// ReadAll: every repo in the space.
	ReadAll
)

// Grants is the set of space: grants a token carries.
type Grants []Grant

// FromScopes parses the space: grants among scopes, granted by self.
// Malformed space: scopes are skipped: they grant nothing.
func FromScopes(scopes []string, self syntax.DID) Grants {
	var grants Grants
	for _, scope := range scopes {
		grant, ok, err := Parse(scope, self)
		if ok && err == nil {
			grants = append(grants, grant)
		}
	}
	return grants
}

// ReadAccess reports how much of space the grants let their holder read:
// read covers every repo and read_self only the holder's own, both regardless
// of collection.
func (gs Grants) ReadAccess(space habitat_syntax.SpaceURI) Read {
	access := ReadNone
	for _, g := range gs {
		if !g.Covers(space) {
			continue
		}
		if slices.Contains(g.Actions, ActionRead) {
			return ReadAll
		}
		if slices.Contains(g.Actions, ActionReadSelf) {
			access = ReadSelf
		}
	}
	return access
}

// Limit is what a credential may read, by space: [Grants] for an OAuth
// token, or [Unrestricted] for credentials that carry no scopes.
type Limit interface {
	ReadAccess(space habitat_syntax.SpaceURI) Read
}

// Unrestricted is the [Limit] of a credential that carries no scopes, such
// as service auth, whose reach is decided by the caller's roles alone.
type Unrestricted struct{}

// ReadAccess implements [Limit].
func (Unrestricted) ReadAccess(habitat_syntax.SpaceURI) Read { return ReadAll }
