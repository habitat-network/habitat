package org

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Create an org that has everyone for instances that don't belong to an org, they just host pear servers for people.
type everyoneOrg struct{}

// IsMember implements Store.
func (e *everyoneOrg) IsMember(ctx context.Context, member syntax.DID) (bool, error) {
	return true, nil
}

var _ Org = &everyoneOrg{}

func NewEveryoneOrg() Org {
	return &everyoneOrg{}
}
