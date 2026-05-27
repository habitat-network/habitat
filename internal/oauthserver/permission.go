package oauthserver

import (
	"fmt"
	"net/url"
	"strings"
)

var validResources = map[string]bool{
	"org":  true,
	"repo": true,
}

type Permission struct {
	Resource   string
	Collection string
	Actions    []string
}

func PermissionFromScope(scope string) (Permission, error) {
	if scope == "" {
		return Permission{}, fmt.Errorf("empty scope")
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
		return Permission{}, fmt.Errorf("scope missing colon: %q", scope)
	}

	resource := positional[:colonIdx]
	if !validResources[resource] {
		return Permission{}, fmt.Errorf("unknown resource: %q", resource)
	}

	collection := positional[colonIdx+1:]
	if collection == "" {
		return Permission{}, fmt.Errorf("scope missing collection: %q", scope)
	}

	var actions []string
	if rawQuery != "" {
		vals, err := url.ParseQuery(rawQuery)
		if err != nil {
			return Permission{}, fmt.Errorf("invalid query in scope %q: %w", scope, err)
		}
		actions = vals["action"]
	}

	return Permission{
		Resource:   resource,
		Collection: collection,
		Actions:    actions,
	}, nil
}

func ScopeMatch(granted, required Permission) bool {
	if granted.Resource != required.Resource {
		return false
	}
	if granted.Collection != "*" && granted.Collection != required.Collection {
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

func ScopesSatisfy(grantedScopes, requiredScopes []string) bool {
	if len(requiredScopes) == 0 {
		return true
	}
	granted := make([]Permission, 0, len(grantedScopes))
	for _, s := range grantedScopes {
		p, err := PermissionFromScope(s)
		if err != nil {
			continue
		}
		granted = append(granted, p)
	}
	for _, req := range requiredScopes {
		requiredP, err := PermissionFromScope(req)
		if err != nil {
			return false
		}
		matched := false
		for _, g := range granted {
			if ScopeMatch(g, requiredP) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
