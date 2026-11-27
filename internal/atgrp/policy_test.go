package atgrp

import (
	"testing"

	"github.com/stretchr/testify/require"

	atgrp_api "github.com/habitat-network/habitat/api/atgrp"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestPolicyGrantContainment(t *testing.T) {
	s, repo := canonicalMembers(t)
	ctx := t.Context()
	space := habitat_syntax.ConstructSpaceURI(testGroup, "com.example.space", "self")
	const held = "space:com.example.space?skey=self&collection=com.example.record&action=update"
	putTestRole(t, repo, "manager", atgrp_api.OrgAtgrpRole{
		CanAssign: []string{"helper"}, Permissions: []string{held},
	})
	putTestRole(t, repo, "helper", atgrp_api.OrgAtgrpRole{})
	admit(t, s, member, "manager")
	actor, err := s.LoadAccess(ctx, testGroup, member)
	require.NoError(t, err)

	policy := func(grants ...spaces.AtgrpGrant) spaces.Policy {
		return spaces.Policy{Type: spaces.PolicyAtgrp, Grants: grants}
	}
	manager := spaces.AtgrpGrant{Role: "manager", Read: true}
	before := policy(manager)
	heldAgain := spaces.AtgrpGrant{Role: "helper", Read: true, Permissions: []string{held}}
	require.NoError(
		t,
		s.ValidatePolicyChange(ctx, space, actor, before, policy(manager, heldAgain)),
	)

	testCases := []struct {
		name   string
		before spaces.Policy
		after  spaces.Policy
		want   error
	}{
		{
			"cannot use the update guard without a canonical policy",
			spaces.Policy{},
			policy(),
			ErrWriteForbidden,
		},
		{
			"update an empty canonical policy with a covered account grant",
			policy(),
			policy(spaces.AtgrpGrant{
				Role: "helper", Permissions: []string{held},
			}),
			nil,
		},
		{
			"an empty canonical policy confers no personal access",
			policy(),
			policy(heldAgain),
			ErrWriteForbidden,
		},
		{"remove an assignable grant", policy(heldAgain), policy(), nil},
		{"remove an unassignable grant", before, policy(), ErrWriteForbidden},
		{"cannot add grants for an unassignable role", policy(), before, ErrWriteForbidden},
		{"cannot widen personal read to write", before, policy(manager, spaces.AtgrpGrant{
			Role: "helper", Write: true,
		}), ErrWriteForbidden},
		{"cannot add an unheld record permission", before, policy(manager, spaces.AtgrpGrant{
			Role:        "helper",
			Permissions: []string{"space:com.example.space?collection=*&action=update"},
		}), ErrWriteForbidden},
		{"cannot grant permission for another space", before, policy(manager, spaces.AtgrpGrant{
			Role:        "helper",
			Permissions: []string{"space:com.example.other?collection=*&action=update"},
		}), ErrInvalidRecord},
		{"cannot assign an invalid role", before, policy(manager, spaces.AtgrpGrant{
			Role: "UPPER", Read: true,
		}), ErrInvalidRecord},
		{"cannot name the same role twice", before, policy(manager, manager), ErrInvalidRecord},
		{"cannot convert to a baseline policy", before, spaces.DefaultPolicy(), ErrWriteForbidden},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.ValidatePolicyChange(ctx, space, actor, tc.before, tc.after)
			if tc.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.want)
			}
		})
	}
	public := before
	public.PublicRead = true
	require.NoError(t, s.ValidatePolicyChange(ctx, space, actor, before, public))
	public.Grants = nil
	require.ErrorIs(
		t, s.ValidatePolicyChange(ctx, space, actor, policy(), public), ErrWriteForbidden,
	)
	require.ErrorIs(
		t,
		s.ValidatePolicyChange(ctx, space, Access{}, before, before),
		ErrWriteForbidden,
	)
	groupAccount, err := s.LoadAccess(ctx, testGroup, testGroup)
	require.NoError(t, err)
	require.ErrorIs(
		t,
		s.ValidatePolicyChange(ctx, space, groupAccount, before, before),
		ErrWriteForbidden,
	)
}
