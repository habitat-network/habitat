package oauthserver

import (
	"fmt"
	"net/url"
	"strings"
)

var validResources = map[string]bool{
	"org": true,
}

type permission struct {
	Resource   string
	Collection string
	Actions    []string
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

	collection := positional[colonIdx+1:]
	if collection == "" {
		return permission{}, fmt.Errorf("scope missing collection: %q", scope)
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
		Resource:   resource,
		Collection: collection,
		Actions:    actions,
	}, nil
}

func scopeMatch(granted, required permission) bool {
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

func scopesSatisfy(grantedScopes, requiredScopes []string) bool {
	if len(requiredScopes) == 0 {
		return true
	}
	granted := make([]permission, 0, len(grantedScopes))
	for _, s := range grantedScopes {
		p, err := permissionFromScope(s)
		if err != nil {
			continue
		}
		granted = append(granted, p)
	}
	for _, req := range requiredScopes {
		requiredP, err := permissionFromScope(req)
		if err != nil {
			return false
		}
		matched := false
		for _, g := range granted {
			if scopeMatch(g, requiredP) {
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
