package oauthserver

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

var (
	OrgScope = "org"
)

var validResources = map[string]bool{
	OrgScope: true,
}

type permission struct {
	Resource  string
	Namespace syntax.NSID
	Actions   []string
}

func permissionFromScope(scope string) (permission, error) {
	if scope == "" {
		return permission{}, fmt.Errorf("empty scope")
	}

	var positional string
	var rawQuery string
	if idx := strings.IndexByte(scope, '?'); idx != -1 {
		positional = scope[:idx]
		rawQuery = scope[idx+1:]
	} else {
		positional = scope
	}

	colonIdx := strings.IndexByte(positional, ':')
	if colonIdx == -1 {
		return permission{}, fmt.Errorf("scope missing colon: %q", scope)
	}

	resource := positional[:colonIdx]
	if !validResources[resource] {
		return permission{}, fmt.Errorf("unknown resource: %q", resource)
	}

	var namespace syntax.NSID
	if positional[colonIdx+1:] == "" {
		return permission{}, fmt.Errorf("scope missing namespace: %q", scope)
	} else if namespace != "*" {
		parsed, err := syntax.ParseNSID(positional[colonIdx+1:])
		if err != nil {
			return permission{}, fmt.Errorf("invalid namespace in scope %q: %w", scope, err)
		}
		namespace = parsed
	}

	var actions []string
	if rawQuery != "" {
		vals, err := url.ParseQuery(rawQuery)
		if err != nil {
			return permission{}, fmt.Errorf("invalid query in scope %q: %w", scope, err)
		}
		actions = vals["action"]
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
	if granted.Namespace != "*" && granted.Namespace != required.Namespace {
		return false
	}
	if len(required.Actions) == 0 {
		return true
	}
	if len(granted.Actions) == 0 {
		return true
	}
	requiredSet := make(map[string]bool, len(required.Actions))
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
