package opensocial

import (
	"context"
	"errors"
	"fmt"

	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// AllowsLegacySpaceCredential is a model gate, not an authorization check.
// A local canonical marker or a staged canonical policy can only deny. A
// missing local space under an unmarked owner may be hosted remotely.
func (s *Store) AllowsLegacySpaceCredential(
	ctx context.Context, uri habitat_syntax.SpaceURI,
) (bool, error) {
	canonical, err := s.IsCanonicalGroup(ctx, uri.SpaceOwner())
	if err != nil || canonical {
		return false, err
	}
	policy, err := s.spacesStore.GetSpacePolicy(ctx, uri)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("check space credential policy: %w", err)
	}
	return policy.Type != spaces.PolicyAtgrp, nil
}
