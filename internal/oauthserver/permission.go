package oauthserver

import (
	"fmt"

	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xmaps"
)

// spaceResource is the OAuth scope resource that grants access to spaces. See
// https://github.com/bluesky-social/proposals/blob/main/0016-permissioned-data/README.md#oauth-scopes
const spaceResource = "space"

// Reserved, non-identifier values a space scope's selectors may take.
const (
	// authoritySelf denotes the granting user's own DID. It is the default
	// authority when the authority param is omitted.
	authoritySelf = "self"
	// wildcard matches any value for the selector it appears on.
	wildcard = "*"
)

// spaceAction is a record-level operation a space scope permits.
type spaceAction string

const (
	ActionReadSelf spaceAction = "read_self"
	ActionRead     spaceAction = "read"
	ActionCreate   spaceAction = "create"
	ActionUpdate   spaceAction = "update"
	ActionDelete   spaceAction = "delete"
)

// defaultActions is the action set a space scope grants when the action param is
// omitted (read is inclusive of read_self).
var defaultActions = []spaceAction{ActionRead, ActionCreate, ActionUpdate, ActionDelete}

func parseAction(s string) (spaceAction, error) {
	switch spaceAction(s) {
	case ActionReadSelf, ActionRead, ActionCreate, ActionUpdate, ActionDelete:
		return spaceAction(s), nil
	default:
		return "", fmt.Errorf("unknown action: %q", s)
	}
}

// spaceManage is a space-level (administrative) operation a space scope permits.
type spaceManage string

const (
	ManageCreate spaceManage = "create"
	ManageUpdate spaceManage = "update"
	ManageDelete spaceManage = "delete"
)

func parseManage(s string) (spaceManage, error) {
	switch spaceManage(s) {
	case ManageCreate, ManageUpdate, ManageDelete:
		return spaceManage(s), nil
	default:
		return "", fmt.Errorf("unknown manage op: %q", s)
	}
}

// spacePermission is a parsed space: OAuth scope. It selects a set of spaces by
// their (Authority, SpaceType, Skey) identifier and states what operations the
// grant permits within them. Fields are exported so the permission can be
// persisted on a session via CBOR. See:
// https://github.com/bluesky-social/proposals/blob/main/0016-permissioned-data/README.md#oauth-scopes
type spacePermission struct {
	// SpaceType is a space-type NSID or wildcard ("*") for any type.
	SpaceType string
	// Authority is a space authority DID, "self" for the granting user's own
	// DID, or wildcard for any authority. Defaults to "self".
	Authority string
	// Skey is a space key or wildcard for any key. Defaults to wildcard.
	Skey string
	// Collections is the set of record collection NSIDs (or wildcard) the grant
	// covers. Empty means the space type's declared default collections.
	Collections []string
	// Actions is the set of permitted record operations. Empty means the
	// default set (read, create, update, delete).
	Actions []spaceAction
	// Manage is the set of permitted space-level operations. Empty by default.
	Manage []spaceManage
}

// permissionFromScope parses a single space: scope string into a spacePermission.
// It is strict: it validates syntax and rejects unknown resources or params. The
// low-level split into resource/positional/params is delegated to indigo's
// auth.ParseGenericScope.
func permissionFromScope(scope string) (spacePermission, error) {
	g, err := auth.ParseGenericScope(scope)
	if err != nil {
		return spacePermission{}, fmt.Errorf("invalid scope %q: %w", scope, err)
	}
	if g.Resource != spaceResource {
		return spacePermission{}, fmt.Errorf("unknown resource %q in scope %q", g.Resource, scope)
	}

	for k := range g.Params {
		switch k {
		case "authority", "skey", "collection", "action", "manage":
		default:
			return spacePermission{}, fmt.Errorf("unsupported param %q in scope %q", k, scope)
		}
	}

	spaceType := g.Positional
	if spaceType == "" {
		return spacePermission{}, fmt.Errorf("scope missing space type: %q", scope)
	}
	if spaceType != wildcard {
		if _, err := syntax.ParseNSID(spaceType); err != nil {
			return spacePermission{}, fmt.Errorf("invalid space type in scope %q: %w", scope, err)
		}
	}

	p := spacePermission{
		SpaceType: spaceType,
		Authority: authoritySelf,
		Skey:      wildcard,
	}

	if g.Params.Has("authority") {
		authority := g.Params.Get("authority")
		if authority != authoritySelf && authority != wildcard {
			if _, err := syntax.ParseDID(authority); err != nil {
				return spacePermission{}, fmt.Errorf("invalid authority in scope %q: %w", scope, err)
			}
		}
		p.Authority = authority
	}

	if g.Params.Has("skey") {
		skey := g.Params.Get("skey")
		if skey != wildcard {
			// A skey has the same syntax requirements as a record key (incl. the
			// 512-char maximum).
			if _, err := syntax.ParseRecordKey(skey); err != nil {
				return spacePermission{}, fmt.Errorf("invalid skey in scope %q: %w", scope, err)
			}
		}
		p.Skey = skey
	}

	for _, coll := range g.Params["collection"] {
		if coll != wildcard {
			if _, err := syntax.ParseNSID(coll); err != nil {
				return spacePermission{}, fmt.Errorf("invalid collection in scope %q: %w", scope, err)
			}
		}
		p.Collections = append(p.Collections, coll)
	}

	for _, a := range g.Params["action"] {
		action, err := parseAction(a)
		if err != nil {
			return spacePermission{}, fmt.Errorf("invalid action in scope %q: %w", scope, err)
		}
		p.Actions = append(p.Actions, action)
	}

	for _, m := range g.Params["manage"] {
		manage, err := parseManage(m)
		if err != nil {
			return spacePermission{}, fmt.Errorf("invalid manage op in scope %q: %w", scope, err)
		}
		p.Manage = append(p.Manage, manage)
	}

	return p, nil
}

// parseSpacePermissions parses each scope into a spacePermission, skipping any
// that are not valid space scopes. Per the proposal, callers skip permissions
// that fail to parse rather than rejecting the whole request.
func parseSpacePermissions(scopes []string) []spacePermission {
	var perms []spacePermission
	for _, scope := range scopes {
		p, err := permissionFromScope(scope)
		if err != nil {
			continue
		}
		perms = append(perms, p)
	}
	return perms
}

// coversSelector reports whether a granted selector value (space type, authority,
// or skey) covers a required one: either the grant uses the wildcard or the
// values are equal. "self" is treated literally here; resolving it to the
// granting user's DID happens at enforcement time, which is out of scope for now.
func coversSelector(granted, required string) bool {
	return granted == wildcard || granted == required
}

// effectiveActions returns the action set a permission confers, applying the
// default set when none are named and expanding read to imply read_self.
func effectiveActions(p spacePermission) map[spaceAction]struct{} {
	actions := p.Actions
	if len(actions) == 0 {
		actions = defaultActions
	}
	set := xmaps.SetFromSlice(actions)
	if _, ok := set[ActionRead]; ok {
		set[ActionReadSelf] = struct{}{}
	}
	return set
}

// coversCollections reports whether the granted scope covers the required
// scope's collections. A grant with collection=* covers any collection; an
// explicit list must be a superset of the required list. A grant that omits
// collection relies on its space type's declared default set, which is not
// resolved here yet, so it covers only a request that likewise omits collection.
func coversCollections(granted, required spacePermission) bool {
	grantedSet := xmaps.SetFromSlice(granted.Collections)
	if _, any := grantedSet[wildcard]; any {
		return true
	}
	requiredSet := xmaps.SetFromSlice(required.Collections)
	if _, any := requiredSet[wildcard]; any {
		return false
	}
	return len(xmaps.Difference(requiredSet, grantedSet)) == 0
}

// scopeMatch reports whether the granted space permission covers the required
// one: the granted scope must select the required scope's spaces and permit all
// of its requested record actions, space-management ops, and collections.
func scopeMatch(granted, required spacePermission) bool {
	if !coversSelector(granted.SpaceType, required.SpaceType) {
		return false
	}
	if !coversSelector(granted.Authority, required.Authority) {
		return false
	}
	if !coversSelector(granted.Skey, required.Skey) {
		return false
	}

	grantedActions := effectiveActions(granted)
	for a := range effectiveActions(required) {
		if _, ok := grantedActions[a]; !ok {
			return false
		}
	}

	// manage ops have neither a wildcard nor a default; required must be a subset.
	grantedManage := xmaps.SetFromSlice(granted.Manage)
	for _, m := range required.Manage {
		if _, ok := grantedManage[m]; !ok {
			return false
		}
	}

	return coversCollections(granted, required)
}

// scopeStrategy is the fosite.ScopeStrategy for Habitat's space scopes: it
// reports whether some granted scope in haystack covers the requested needle.
func scopeStrategy(haystack []string, needle string) bool {
	requiredP, err := permissionFromScope(needle)
	if err != nil {
		return false
	}
	for _, granted := range haystack {
		grantedP, err := permissionFromScope(granted)
		if err != nil {
			continue
		}
		if scopeMatch(grantedP, requiredP) {
			return true
		}
	}
	return false
}
