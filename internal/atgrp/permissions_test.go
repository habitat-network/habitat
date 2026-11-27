package atgrp

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestPermissionUnion(t *testing.T) {
	group := syntax.DID("did:plc:group")
	meta := habitat_syntax.ConstructSpaceURI(group, "org.atgrp.meta", "self")
	externalMeta := habitat_syntax.ConstructSpaceURI("did:plc:other", "org.atgrp.meta", "self")
	memberSpace := habitat_syntax.ConstructSpaceURI(group, "org.atgrp.members", "self")
	inbox := habitat_syntax.ConstructSpaceURI("did:plc:other", "org.atgrp.inbox", "self")
	testCases := []struct {
		name   string
		scopes []string
		allows func(PermissionSet) bool
		want   bool
	}{
		{
			"published permission set permits role update",
			[]string{"include:org.atgrp.authManageAdmin"},
			func(s PermissionSet) bool {
				return s.AllowsRecord(memberSpace, "org.atgrp.role", "update")
			},
			true,
		},
		{
			"published permission set preserves inbox read_self",
			[]string{"include:org.atgrp.authManageMembers"},
			func(s PermissionSet) bool {
				return s.AllowsRecord(inbox, "", "read_self") && !s.AllowsRecord(inbox, "", "read")
			},
			true,
		},
		{
			"read ignores collection and implies read_self",
			[]string{"space:org.atgrp.meta?collection=org.atgrp.about&action=read"},
			func(s PermissionSet) bool { return s.AllowsRecord(meta, "org.atgrp.config", "read_self") },
			true,
		},
		{
			"read_self does not permit reading all",
			[]string{"space:org.atgrp.meta?action=read_self"},
			func(s PermissionSet) bool { return s.AllowsRecord(meta, "", "read") },
			false,
		},
		{
			"implicit authority means group itself",
			[]string{"space:org.atgrp.meta"},
			func(s PermissionSet) bool { return s.AllowsRecord(externalMeta, "org.atgrp.about", "update") },
			false,
		},
		{
			"wildcard type without a collection does not grant writes",
			[]string{"space:*?manage=update"},
			func(s PermissionSet) bool { return s.AllowsRecord(meta, "org.atgrp.about", "update") },
			false,
		},
		{
			"omitted collection finds the declared collection",
			[]string{"space:org.atgrp.meta"},
			func(s PermissionSet) bool { return s.AllowsRecord(meta, "org.atgrp.about", "update") },
			true,
		},
		{
			"grant's namespace does not authorize undeclared collections",
			[]string{"space:org.atgrp.meta"},
			func(s PermissionSet) bool { return s.AllowsRecord(meta, "org.atgrp.role", "update") },
			false,
		},
		{
			"space management does not follow record write access",
			[]string{"space:*?collection=*"},
			func(s PermissionSet) bool { return s.AllowsManage(meta, "update") },
			false,
		},
		{
			"unions cover separate parts of a required permission",
			[]string{
				"space:org.atgrp.members?collection=org.atgrp.role&action=create",
				"space:org.atgrp.members?collection=org.atgrp.role&action=update",
			},
			func(s PermissionSet) bool {
				return s.Covers(
					group,
					"space:org.atgrp.members?collection=org.atgrp.role&action=create&action=update",
				)
			},
			true,
		},
		{
			"concrete scope cannot delegate a wildcard",
			[]string{"space:org.atgrp.members?skey=self&collection=org.atgrp.role&action=update"},
			func(s PermissionSet) bool {
				return s.Covers(
					group,
					"space:org.atgrp.members?collection=org.atgrp.role&action=update",
				)
			},
			false,
		},
		{
			"malformed scopes never expand to defaults",
			[]string{
				"space:org.atgrp.members?action=%not",
				"space:org.atgrp.members?action=read-self",
				"space:org.atgrp.members?authority=*&authority=self",
				"space:org.atgrp.members?manage=reset",
				"space:org.atgrp.members?collections=",
				"include:org.atgrp.authManageAdmin?aud=invalid",
			},
			func(s PermissionSet) bool {
				return s.AllowsRecord(memberSpace, "org.atgrp.role", "update")
			},
			false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.allows(ParsePermissions(group, tc.scopes)))
		})
	}
}

func TestIntersectionNarrowsBothSides(t *testing.T) {
	group := syntax.DID("did:plc:group")
	held := ParsePermissions(group, []string{"space:org.atgrp.meta?collection=*&action=update"})
	wanted := ParsePermissions(
		group,
		[]string{"space:*?skey=self&collection=org.atgrp.about&action=update"},
	)

	p, ok := held.grants[0].intersect(wanted.grants[0])
	require.True(t, ok)
	scopes := narrowScopes(group, map[permission]bool{p: true})
	require.Equal(
		t,
		[]string{"space:org.atgrp.meta?action=update&collection=org.atgrp.about&skey=self"},
		scopes,
	)
	result := ParsePermissions(group, scopes)
	meta := habitat_syntax.ConstructSpaceURI(group, "org.atgrp.meta", "self")
	require.True(t, result.AllowsRecord(meta, "org.atgrp.about", "update"))
	require.False(t, result.AllowsRecord(meta, "org.atgrp.about", "create"))
	require.False(t, result.AllowsRecord(meta, "org.atgrp.config", "update"))
}

func TestIntersectionDoesNotManufactureRead(t *testing.T) {
	group := syntax.DID("did:plc:group")
	space := habitat_syntax.ConstructSpaceURI(group, "org.atgrp.members", "self")
	manage := spacePermission(space, "manage:update")
	require.Empty(t, narrowScopes(group, map[permission]bool{manage: true}))
	read := spacePermission(space, "read_self")
	scopes := narrowScopes(group, map[permission]bool{manage: true, read: true})
	result := ParsePermissions(group, scopes)
	require.True(t, result.AllowsManage(space, "update"))
	require.True(t, result.AllowsRecord(space, "", "read_self"))
	require.False(t, result.AllowsRecord(space, "", "read"))
}

func TestGroupSessionDoesNotOfferModerationOrDirectAdmission(t *testing.T) {
	group := syntax.DID("did:plc:group")
	space := habitat_syntax.ConstructSpaceURI(group, "org.atgrp.members", "self")
	allowed := make(map[permission]bool)
	for _, p := range ParsePermissions(group, []string{"include:org.atgrp.authManageAdmin"}).grants {
		allowed[p] = true
	}
	result := ParsePermissions(group, narrowScopes(group, allowed))
	require.True(t, result.AllowsRecord(space, "org.atgrp.membership", "update"))
	require.False(t, result.AllowsRecord(space, "org.atgrp.membership", "create"))
	require.False(t, result.AllowsRecord(space, "org.atgrp.membership", "delete"))
	require.False(t, result.AllowsRecord(space, "com.atproto.label.record", "create"))
	require.False(t, result.Covers(group, "rpc:org.atgrp.mod.querySubjects?aud=*"))
}

func TestCompoundRPCPermission(t *testing.T) {
	group := syntax.DID("did:plc:group")
	held := ParsePermissions(group, []string{"rpc?lxm=*&aud=did:web:example.com%23svc_groups"})
	require.True(
		t,
		held.Covers(group, "rpc:org.atgrp.listJoinRequests?aud=did:web:example.com%23svc_groups"),
	)
	require.False(t, held.Covers(group, "rpc:org.atgrp.listJoinRequests?aud=*"))
	require.False(
		t,
		held.Covers(
			group,
			"rpc:org.atgrp.listJoinRequests?aud=did:web:other.example.com%23svc_groups",
		),
	)
}
