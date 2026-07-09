package oauthserver

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPermissionFromScope(t *testing.T) {
	tests := []struct {
		name    string
		scope   string
		want    spacePermission
		wantErr bool
	}{
		{
			name:  "bare space type defaults authority to self and skey to wildcard",
			scope: "space:com.example.bookmarks",
			want: spacePermission{
				SpaceType: "com.example.bookmarks",
				Authority: authoritySelf,
				Skey:      wildcard,
			},
		},
		{
			name:  "wildcard space type",
			scope: "space:*",
			want: spacePermission{
				SpaceType: wildcard,
				Authority: authoritySelf,
				Skey:      wildcard,
			},
		},
		{
			name:  "authority wildcard",
			scope: "space:com.atmoboards.forum?authority=*",
			want: spacePermission{
				SpaceType: "com.atmoboards.forum",
				Authority: wildcard,
				Skey:      wildcard,
			},
		},
		{
			name:  "explicit authority did and skey",
			scope: "space:com.atmoboards.forum?authority=did:plc:abc123&skey=default",
			want: spacePermission{
				SpaceType: "com.atmoboards.forum",
				Authority: "did:plc:abc123",
				Skey:      "default",
			},
		},
		{
			name:  "collections actions and manage",
			scope: "space:com.atmoboards.forum?authority=*&collection=com.atmoboards.thread&action=create&action=update&manage=delete",
			want: spacePermission{
				SpaceType:   "com.atmoboards.forum",
				Authority:   wildcard,
				Skey:        wildcard,
				Collections: []string{"com.atmoboards.thread"},
				Actions:     []spaceAction{ActionCreate, ActionUpdate},
				Manage:      []spaceManage{ManageDelete},
			},
		},
		{
			name:  "collection wildcard and read_self",
			scope: "space:com.atmoboards.forum?authority=*&action=read_self&collection=*",
			want: spacePermission{
				SpaceType:   "com.atmoboards.forum",
				Authority:   wildcard,
				Skey:        wildcard,
				Collections: []string{wildcard},
				Actions:     []spaceAction{ActionReadSelf},
			},
		},
		{
			name:    "unknown resource",
			scope:   "org:*",
			wantErr: true,
		},
		{
			name:    "missing space type",
			scope:   "space:",
			wantErr: true,
		},
		{
			name:    "no positional value",
			scope:   "space",
			wantErr: true,
		},
		{
			name:    "invalid space type nsid",
			scope:   "space:not-an-nsid",
			wantErr: true,
		},
		{
			name:    "invalid authority did",
			scope:   "space:com.example.type?authority=not-a-did",
			wantErr: true,
		},
		{
			name:    "unknown action",
			scope:   "space:com.example.type?action=bogus",
			wantErr: true,
		},
		{
			name:    "unknown manage op",
			scope:   "space:com.example.type?manage=bogus",
			wantErr: true,
		},
		{
			name:    "unsupported param",
			scope:   "space:com.example.type?bogus=1",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := permissionFromScope(tt.scope)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseSpacePermissionsSkipsInvalid(t *testing.T) {
	perms := parseSpacePermissions([]string{
		"space:com.example.bookmarks",
		"org:*", // not a space scope, skipped
		"space:*",
	})
	require.Equal(t, []spacePermission{
		{SpaceType: "com.example.bookmarks", Authority: authoritySelf, Skey: wildcard},
		{SpaceType: wildcard, Authority: authoritySelf, Skey: wildcard},
	}, perms)
}

func TestScopeMatch(t *testing.T) {
	tests := []struct {
		name     string
		granted  spacePermission
		required spacePermission
		want     bool
	}{
		{
			name:     "space type wildcard covers concrete type",
			granted:  spacePermission{SpaceType: wildcard, Authority: wildcard, Skey: wildcard},
			required: spacePermission{SpaceType: "com.example.type", Authority: wildcard, Skey: wildcard},
			want:     true,
		},
		{
			name:     "different space type no match",
			granted:  spacePermission{SpaceType: "com.example.a", Authority: wildcard, Skey: wildcard},
			required: spacePermission{SpaceType: "com.example.b", Authority: wildcard, Skey: wildcard},
			want:     false,
		},
		{
			name:     "authority wildcard covers concrete did",
			granted:  spacePermission{SpaceType: "com.example.type", Authority: wildcard, Skey: wildcard},
			required: spacePermission{SpaceType: "com.example.type", Authority: "did:plc:abc", Skey: wildcard},
			want:     true,
		},
		{
			name:     "concrete authority does not cover wildcard",
			granted:  spacePermission{SpaceType: "com.example.type", Authority: "did:plc:abc", Skey: wildcard},
			required: spacePermission{SpaceType: "com.example.type", Authority: wildcard, Skey: wildcard},
			want:     false,
		},
		{
			name:     "skey wildcard covers concrete skey",
			granted:  spacePermission{SpaceType: "com.example.type", Authority: authoritySelf, Skey: wildcard},
			required: spacePermission{SpaceType: "com.example.type", Authority: authoritySelf, Skey: "default"},
			want:     true,
		},
		{
			name:     "different skey no match",
			granted:  spacePermission{SpaceType: "com.example.type", Authority: authoritySelf, Skey: "a"},
			required: spacePermission{SpaceType: "com.example.type", Authority: authoritySelf, Skey: "b"},
			want:     false,
		},
		{
			name:     "default actions cover default actions",
			granted:  spacePermission{SpaceType: wildcard, Authority: wildcard, Skey: wildcard},
			required: spacePermission{SpaceType: "com.example.type", Authority: wildcard, Skey: wildcard},
			want:     true,
		},
		{
			name: "read implies read_self",
			granted: spacePermission{
				SpaceType: wildcard, Authority: wildcard, Skey: wildcard,
				Actions: []spaceAction{ActionRead},
			},
			required: spacePermission{
				SpaceType: "com.example.type", Authority: wildcard, Skey: wildcard,
				Actions: []spaceAction{ActionReadSelf},
			},
			want: true,
		},
		{
			name: "read_self does not imply read",
			granted: spacePermission{
				SpaceType: wildcard, Authority: wildcard, Skey: wildcard,
				Actions: []spaceAction{ActionReadSelf},
			},
			required: spacePermission{
				SpaceType: "com.example.type", Authority: wildcard, Skey: wildcard,
				Actions: []spaceAction{ActionRead},
			},
			want: false,
		},
		{
			name: "missing required action fails",
			granted: spacePermission{
				SpaceType: wildcard, Authority: wildcard, Skey: wildcard,
				Actions: []spaceAction{ActionRead},
			},
			required: spacePermission{
				SpaceType: "com.example.type", Authority: wildcard, Skey: wildcard,
				Actions: []spaceAction{ActionCreate},
			},
			want: false,
		},
		{
			name: "manage subset covered",
			granted: spacePermission{
				SpaceType: wildcard, Authority: wildcard, Skey: wildcard,
				Manage: []spaceManage{ManageUpdate, ManageDelete},
			},
			required: spacePermission{
				SpaceType: "com.example.type", Authority: wildcard, Skey: wildcard,
				Manage: []spaceManage{ManageUpdate},
			},
			want: true,
		},
		{
			name: "missing required manage op fails",
			granted: spacePermission{
				SpaceType: wildcard, Authority: wildcard, Skey: wildcard,
				Manage: []spaceManage{ManageUpdate},
			},
			required: spacePermission{
				SpaceType: "com.example.type", Authority: wildcard, Skey: wildcard,
				Manage: []spaceManage{ManageDelete},
			},
			want: false,
		},
		{
			name: "collection wildcard covers concrete collection",
			granted: spacePermission{
				SpaceType: wildcard, Authority: wildcard, Skey: wildcard,
				Collections: []string{wildcard},
			},
			required: spacePermission{
				SpaceType: "com.example.type", Authority: wildcard, Skey: wildcard,
				Collections: []string{"com.example.record"},
			},
			want: true,
		},
		{
			name: "explicit collection superset covers subset",
			granted: spacePermission{
				SpaceType: wildcard, Authority: wildcard, Skey: wildcard,
				Collections: []string{"com.example.a", "com.example.b"},
			},
			required: spacePermission{
				SpaceType: "com.example.type", Authority: wildcard, Skey: wildcard,
				Collections: []string{"com.example.a"},
			},
			want: true,
		},
		{
			name: "collection not in granted set fails",
			granted: spacePermission{
				SpaceType: wildcard, Authority: wildcard, Skey: wildcard,
				Collections: []string{"com.example.a"},
			},
			required: spacePermission{
				SpaceType: "com.example.type", Authority: wildcard, Skey: wildcard,
				Collections: []string{"com.example.b"},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, scopeMatch(tt.granted, tt.required))
		})
	}
}

func TestScopeStrategy(t *testing.T) {
	t.Run("wildcard satisfies concrete", func(t *testing.T) {
		require.True(t, scopeStrategy([]string{"space:*?authority=*"}, "space:com.example.type?authority=*"))
	})
	t.Run("exact match", func(t *testing.T) {
		require.True(t, scopeStrategy([]string{"space:com.example.type"}, "space:com.example.type"))
	})
	t.Run("missing scope", func(t *testing.T) {
		require.False(t, scopeStrategy([]string{"space:com.example.other"}, "space:com.example.type"))
	})
	t.Run("empty granted not satisfied", func(t *testing.T) {
		require.False(t, scopeStrategy([]string{}, "space:com.example.type"))
	})
	t.Run("invalid needle not satisfied", func(t *testing.T) {
		require.False(t, scopeStrategy([]string{"space:*"}, "org:*"))
	})
	t.Run("needle in multi-item haystack", func(t *testing.T) {
		require.True(t, scopeStrategy(
			[]string{"space:com.example.other", "space:com.example.type"},
			"space:com.example.type",
		))
	})
}
