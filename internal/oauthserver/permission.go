package oauthserver

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type scopeResource string
type scopeAction string

var (
	ResourceOrg  scopeResource = "org"
	ActionCreate scopeAction   = "create"
	ActionUpdate scopeAction   = "update"
)

func parseResource(scope string) (scopeResource, error) {
	switch scopeResource(scope) {
	case ResourceOrg:
		return scopeResource(scope), nil
	default:
		return "", fmt.Errorf("unknown resource: %q", scope)
	}
}

func parseAction(scope string) (scopeAction, error) {
	switch scopeAction(scope) {
	case ActionCreate, ActionUpdate:
		return scopeAction(scope), nil
	default:
		return "", fmt.Errorf("unknown action: %q", scope)
	}
}

type permission struct {
	Resource  scopeResource
	Namespace syntax.NSID
	Actions   []scopeAction
}

func permissionFromScope(scope string) (permission, error) {
	if scope == "" {
		return permission{}, fmt.Errorf("empty scope")
	}

	var positional string
	var rawQuery string
	if before, after, ok := strings.Cut(scope, "?"); ok {
		positional = before
		rawQuery = after
	} else {
		positional = scope
	}

	before, after, ok := strings.Cut(positional, ":")
	if !ok {
		return permission{}, fmt.Errorf("scope missing colon: %q", scope)
	}

	resource, err := parseResource(before)
	if err != nil {
		return permission{}, fmt.Errorf("invalid resource in scope %q: %w", scope, err)
	}

	var namespace syntax.NSID
	if after == "" {
		return permission{}, fmt.Errorf("scope missing namespace: %q", scope)
	} else if after != "*" {
		parsed, err := syntax.ParseNSID(after)
		if err != nil {
			return permission{}, fmt.Errorf("invalid namespace in scope %q: %w", scope, err)
		}
		namespace = parsed
	}

	var actions []scopeAction
	if rawQuery != "" {
		vals, err := url.ParseQuery(rawQuery)
		if err != nil {
			return permission{}, fmt.Errorf("invalid query in scope %q: %w", scope, err)
		}
		for _, action := range vals["action"] {
			a, err := parseAction(action)
			if err != nil {
				return permission{}, fmt.Errorf("invalid action in scope %q: %w", scope, err)
			}
			actions = append(actions, a)
		}
	}

	return permission{
		Resource:  resource,
		Namespace: namespace,
		Actions:   actions,
	}, nil
}

func scopeMatch(granted, required permission) bool {
	if granted.Resource != required.Resource {
		return false
	}
	if granted.Namespace != "" && granted.Namespace != required.Namespace {
		return false
	}
	if len(required.Actions) == 0 {
		return true
	}
	if len(granted.Actions) == 0 {
		return true
	}
	requiredSet := make(map[scopeAction]bool, len(required.Actions))
	for _, a := range required.Actions {
		requiredSet[a] = true
	}
	for _, a := range granted.Actions {
		delete(requiredSet, a)
	}
	return len(requiredSet) == 0
}

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
