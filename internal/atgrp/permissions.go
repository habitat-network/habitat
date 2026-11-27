package atgrp

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/lexicons"
)

const wildcard = "*"

// Each permission is one operation. Splitting compound grants lets two roles
// together cover the actions or collections in one requested scope.
type permission struct {
	resource   string
	primary    string // space type or RPC method
	secondary  string // space authority or RPC audience
	key        string
	operation  string // record action or "manage:<operation>"
	collection string
}

var (
	roleDeclarations, declaredCollections = loadDeclarations()
	recordActions                         = []string{"read", "create", "update", "delete"}
)

type lexPermission struct {
	Resource   string   `json:"resource"`
	SpaceType  string   `json:"spaceType"`
	Authority  string   `json:"authority"`
	Skey       string   `json:"skey"`
	Collection []string `json:"collection"`
	Action     []string `json:"action"`
	Manage     []string `json:"manage"`
	Lxm        []string `json:"lxm"`
	Aud        string   `json:"aud"`
}

func loadDeclarations() (map[string][]lexPermission, map[string][]string) {
	dir := "org/atgrp"
	entries, err := lexicons.AtgrpPermissions.ReadDir(dir)
	if err != nil {
		panic(err)
	}
	sets := make(map[string][]lexPermission)
	collections := make(map[string][]string)
	for _, entry := range entries {
		raw, err := lexicons.AtgrpPermissions.ReadFile(dir + "/" + entry.Name())
		if err != nil {
			panic(err)
		}
		var doc struct {
			ID   string `json:"id"`
			Defs struct {
				Main struct {
					Type        string          `json:"type"`
					Permissions []lexPermission `json:"permissions"`
					Collections []string        `json:"collections"`
				} `json:"main"`
			} `json:"defs"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			panic(err)
		}
		switch doc.Defs.Main.Type {
		case "permission-set":
			sets[doc.ID] = doc.Defs.Main.Permissions
		case "space":
			collections[doc.ID] = doc.Defs.Main.Collections
		default:
			panic("unexpected group permission declaration " + doc.ID)
		}
	}
	return sets, collections
}

// PermissionSet is the union of the group account's permissions expressed by
// OAuth scopes. A group's permissions never substitute for token scopes.
type PermissionSet struct {
	grants []permission
	all    bool
}

// ParsePermissions expands the supported atproto space and RPC scopes and
// the locally published group permission sets. Invalid scopes grant nothing.
func ParsePermissions(group syntax.DID, scopes []string) PermissionSet {
	var out PermissionSet
	for _, scope := range scopes {
		grants, err := parseScope(group, scope)
		if err == nil {
			out.grants = append(out.grants, grants...)
		}
	}
	return out
}

// Covers reports whether the union covers every operation in scope.
func (s PermissionSet) Covers(group syntax.DID, scope string) bool {
	wanted, err := parseScope(group, scope)
	if err != nil || len(wanted) == 0 {
		return false
	}
	for _, want := range wanted {
		if !s.all && !slices.ContainsFunc(s.grants, func(held permission) bool {
			return held.covers(want)
		}) {
			return false
		}
	}
	return true
}

// AllowsRecord checks one concrete space record operation. Reads ignore
// collection, and read includes read_self.
func (s PermissionSet) AllowsRecord(
	space habitat_syntax.SpaceURI, collection syntax.NSID, action string,
) bool {
	if !slices.Contains(recordActions, action) && action != "read_self" {
		return false
	}
	want := spacePermission(space, action)
	if action != "read" && action != "read_self" {
		want.collection = collection.String()
		if want.collection == "" || want.collection == wildcard {
			return false
		}
	}
	return s.allows(want)
}

// AllowsManage checks a concrete operation on the space itself, independently
// of which records the caller may write in it.
func (s PermissionSet) AllowsManage(space habitat_syntax.SpaceURI, action string) bool {
	if !isWriteAction(action) {
		return false
	}
	return s.allows(spacePermission(space, "manage:"+action))
}

func spacePermission(space habitat_syntax.SpaceURI, operation string) permission {
	return permission{
		resource: "space", primary: space.SpaceType().String(),
		secondary: space.SpaceOwner().String(), key: space.Skey().String(), operation: operation,
	}
}

func (s PermissionSet) allows(want permission) bool {
	return s.all || slices.ContainsFunc(s.grants, func(held permission) bool {
		return held.covers(want)
	})
}

func (p permission) covers(want permission) bool {
	if p.resource != want.resource ||
		!selectorCovers(p.primary, want.primary) ||
		!selectorCovers(p.secondary, want.secondary) ||
		!selectorCovers(p.key, want.key) ||
		!selectorCovers(p.collection, want.collection) {
		return false
	}
	return p.operation == want.operation || p.operation == "read" && want.operation == "read_self"
}

func selectorCovers(held, want string) bool {
	return held == want || held == wildcard
}

func (p permission) intersect(other permission) (permission, bool) {
	if p.covers(other) {
		return other, true
	}
	if other.covers(p) {
		return p, true
	}
	if p.resource != other.resource {
		return permission{}, false
	}
	result := p
	for _, pair := range []struct {
		got    *string
		wanted string
	}{
		{&result.primary, other.primary},
		{&result.secondary, other.secondary},
		{&result.key, other.key},
		{&result.collection, other.collection},
	} {
		switch {
		case *pair.got == wildcard:
			*pair.got = pair.wanted
		case !selectorCovers(pair.wanted, *pair.got):
			return permission{}, false
		}
	}
	switch {
	case p.operation == other.operation:
	case p.operation == "read" && other.operation == "read_self":
		result.operation = other.operation
	case p.operation == "read_self" && other.operation == "read":
	default:
		return permission{}, false
	}
	return result, true
}

func parseScope(group syntax.DID, scope string) ([]permission, error) {
	if scope == "" || len(scope) > 2000 || strings.ContainsAny(scope, " \t\n\r") {
		return nil, fmt.Errorf("invalid permission length or whitespace")
	}
	head, query, _ := strings.Cut(scope, "?")
	kind, name, _ := strings.Cut(head, ":")
	name, err := url.PathUnescape(name)
	if err != nil {
		return nil, err
	}
	params, err := url.ParseQuery(query)
	if err != nil {
		return nil, err
	}
	switch kind {
	case "include":
		set, ok := roleDeclarations[name]
		if !ok || !validParams(params, nil, nil) {
			return nil, fmt.Errorf("unsupported permission set %q", name)
		}
		var grants []permission
		for _, entry := range set {
			switch entry.Resource {
			case "space":
				entryGrants, err := parseSpacePermission(group, entry)
				if err != nil {
					return nil, err
				}
				grants = append(grants, entryGrants...)
			case "rpc":
				for _, method := range entry.Lxm {
					p, err := parseRPC(method, entry.Aud)
					if err != nil {
						return nil, err
					}
					grants = append(grants, p)
				}
			default:
				return nil, fmt.Errorf("unsupported permission set resource %q", entry.Resource)
			}
		}
		return grants, nil
	case "space":
		if !validParams(
			params,
			[]string{"authority", "skey"},
			[]string{"collection", "action", "manage"},
		) {
			return nil, fmt.Errorf("invalid space selectors")
		}
		return parseSpacePermission(group, lexPermission{
			SpaceType: name, Authority: params.Get("authority"), Skey: params.Get("skey"),
			Collection: params["collection"], Action: params["action"], Manage: params["manage"],
		})
	case "rpc":
		if !validParams(params, []string{"aud"}, []string{"lxm"}) {
			return nil, fmt.Errorf("invalid RPC selectors")
		}
		methods := params["lxm"]
		if name != "" {
			if len(methods) != 0 {
				return nil, fmt.Errorf("RPC method uses both positional and query selectors")
			}
			methods = []string{name}
		}
		if len(methods) == 0 {
			return nil, fmt.Errorf("missing RPC method")
		}
		var out []permission
		for _, method := range methods {
			p, err := parseRPC(method, params.Get("aud"))
			if err != nil {
				return nil, err
			}
			out = append(out, p)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported permission resource %q", kind)
	}
}

func validParams(params url.Values, single, multiple []string) bool {
	for key, values := range params {
		valid := slices.Contains(single, key) && len(values) == 1 || slices.Contains(multiple, key)
		if !valid || slices.Contains(values, "") {
			return false
		}
	}
	return true
}

func parseRPC(method, audience string) (permission, error) {
	if method != wildcard {
		if _, err := syntax.ParseNSID(method); err != nil {
			return permission{}, err
		}
	}
	if audience != wildcard {
		did, fragment, _ := strings.Cut(audience, "#")
		if _, err := syntax.ParseDID(did); err != nil {
			return permission{}, err
		}
		if fragment == "" || strings.Contains(fragment, "#") {
			return permission{}, fmt.Errorf("RPC audience must identify a service")
		}
	} else if method == wildcard {
		return permission{}, fmt.Errorf("unbounded RPC permission")
	}
	return permission{resource: "rpc", primary: method, secondary: audience}, nil
}

func parseSpacePermission(group syntax.DID, scope lexPermission) ([]permission, error) {
	p := permission{
		resource: "space", primary: scope.SpaceType, secondary: scope.Authority, key: scope.Skey,
	}
	if p.primary != wildcard {
		if _, err := syntax.ParseNSID(p.primary); err != nil {
			return nil, err
		}
	}
	switch p.secondary {
	case "", "self":
		p.secondary = group.String()
	case wildcard:
	default:
		if _, err := syntax.ParseDID(p.secondary); err != nil {
			return nil, err
		}
	}
	switch p.key {
	case "":
		p.key = wildcard
	case wildcard:
	default:
		if _, err := syntax.ParseRecordKey(p.key); err != nil {
			return nil, err
		}
	}

	collections := scope.Collection
	if len(collections) == 0 {
		// A wildcard type has no default write collections. Unknown modality
		// declarations require explicit collections until a resolver is wired.
		collections = declaredCollections[p.primary]
	}
	for _, collection := range collections {
		if collection != wildcard {
			if _, err := syntax.ParseNSID(collection); err != nil {
				return nil, err
			}
		}
	}
	var grants []permission
	actions := scope.Action
	if len(actions) == 0 {
		actions = recordActions
	}
	for _, action := range actions {
		grant := p
		grant.operation = action
		switch {
		case action == "read" || action == "read_self":
			grants = append(grants, grant)
		case isWriteAction(action):
			for _, collection := range collections {
				grant.collection = collection
				grants = append(grants, grant)
			}
		default:
			return nil, fmt.Errorf("invalid record action %q", action)
		}
	}
	for _, manage := range scope.Manage {
		if !isWriteAction(manage) {
			return nil, fmt.Errorf("invalid space management action %q", manage)
		}
		grant := p
		grant.operation = "manage:" + manage
		grants = append(grants, grant)
	}
	return grants, nil
}

func isWriteAction(action string) bool {
	return action == "create" || action == "update" || action == "delete"
}

func narrowScopes(group syntax.DID, allowed map[permission]bool) []string {
	scopes := make(map[string]bool)
	for grant := range allowed {
		if grant.resource == "rpc" {
			// Join-request queues and moderation are not offered by this host.
			if strings.HasPrefix(grant.primary, "org.atgrp.mod.") ||
				grant.primary == "org.atgrp.listJoinRequests" ||
				grant.primary == "org.atgrp.denyJoinRequest" ||
				grant.primary == "org.atgrp.cancelJoinRequest" {
				continue
			}
			query := url.Values{"aud": {grant.secondary}}
			scopes["rpc:"+grant.primary+"?"+query.Encode()] = true
			continue
		}
		if strings.HasPrefix(grant.operation, "manage:") {
			continue
		}
		switch grant.collection {
		case "com.atproto.label.record", "org.atgrp.labeler":
			continue
		case "org.atgrp.membership":
			if grant.operation != "update" {
				// Invite redemption and personal leave manage membership
				// creation and deletion; group sessions edit existing roles.
				continue
			}
		}
		query := url.Values{"action": {grant.operation}}
		if grant.secondary != group.String() {
			query.Set("authority", grant.secondary)
		}
		if grant.key != wildcard {
			query.Set("skey", grant.key)
		}
		if grant.collection != "" {
			query.Set("collection", grant.collection)
		}
		// Every wire space scope includes a record operation. Emit management
		// only alongside a record operation allowed on the same selector.
		// Don't add a read grant to express a management-only intersection.
		for _, verb := range recordActions[1:] {
			manage := grant
			manage.operation = "manage:" + verb
			manage.collection = ""
			if coversManage(allowed, manage) {
				query.Add("manage", verb)
			}
		}
		scopes["space:"+grant.primary+"?"+query.Encode()] = true
	}
	out := make([]string, 0, len(scopes))
	for scope := range scopes {
		out = append(out, scope)
	}
	slices.Sort(out)
	return out
}

func coversManage(allowed map[permission]bool, want permission) bool {
	for _, primary := range [2]string{want.primary, wildcard} {
		for _, secondary := range [2]string{want.secondary, wildcard} {
			for _, key := range [2]string{want.key, wildcard} {
				if allowed[permission{
					resource: "space", primary: primary, secondary: secondary,
					key: key, operation: want.operation,
				}] {
					return true
				}
			}
		}
	}
	return false
}
