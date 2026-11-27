package opensocial

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// SessionScopes resolves who acts, and with which scopes, when subject calls a
// method on group. authorizer is the member who approved an OAuth session
// acting as the group, and granted the scopes of its token.
//
// A member acts as themselves with their roles' scopes. A session acting as
// the group acts as its authorizer, with the authorizer's scopes as they are
// now, so demoting or ejecting them narrows every session they approved at
// once. When the token asked for specific scopes, they narrow it further. A
// session with no recorded authorizer, issued before sessions kept one, acts
// as the group itself with every scope.
func (s *Store) SessionScopes(
	ctx context.Context,
	group, subject, authorizer syntax.DID,
	granted []string,
) (actor syntax.DID, scopes ScopeSet, err error) {
	if subject != group {
		scopes, err = s.UserScopes(ctx, group, subject)
		return subject, scopes, err
	}
	if authorizer == "" {
		return group, AllScopes(), nil
	}
	scopes, err = s.UserScopes(ctx, group, authorizer)
	if err != nil {
		return "", ScopeSet{}, err
	}
	if RestrictsSession(granted) {
		scopes = scopes.Within(NewScopeSet(granted))
	}
	return authorizer, scopes, nil
}
