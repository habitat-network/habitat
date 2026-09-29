package oauthserver_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/oauthserver"
)

func TestPermissionFromScope(t *testing.T) {
	tests := []struct {
		name    string
		scope   string
		want    oauthserver.TestPermission
		wantErr bool
	}{
		{
			name:  "all spaces types wildcard",
			scope: "org:*",
			want:  oauthserver.TestPermission{Resource: "org"},
		},
		{
			name:  "single space type",
			scope: "org:com.example.type",
			want:  oauthserver.TestPermission{Resource: "org", Namespace: "com.example.type"},
		},
		{
			name:  "single space with actions",
			scope: "org:com.example.type?action=create&action=update",
			want: oauthserver.TestPermission{
				Resource:  "org",
				Namespace: "com.example.type",
				Actions: []oauthserver.TestScopeAction{
					oauthserver.ActionCreate,
					oauthserver.ActionUpdate,
				},
			},
		},
		{
			name:    "unknown resource",
			scope:   "invalid:*",
			wantErr: true,
		},
		{
			name:    "empty scope",
			scope:   "",
			wantErr: true,
		},
		{
			name:    "no positional value",
			scope:   "org",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := oauthserver.PermissionFromScope(tt.scope)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestScopeMatch(t *testing.T) {
	tests := []struct {
		name     string
		granted  oauthserver.TestPermission
		required oauthserver.TestPermission
		want     bool
	}{
		{
			name:     "wildcard matches any space type",
			granted:  oauthserver.TestPermission{Resource: "org"},
			required: oauthserver.TestPermission{Resource: "org", Namespace: "com.example.type"},
			want:     true,
		},
		{
			name:     "exact match",
			granted:  oauthserver.TestPermission{Resource: "org", Namespace: "com.example.type"},
			required: oauthserver.TestPermission{Resource: "org", Namespace: "com.example.type"},
			want:     true,
		},
		{
			name:     "different collection no match",
			granted:  oauthserver.TestPermission{Resource: "org", Namespace: "com.example.type"},
			required: oauthserver.TestPermission{Resource: "org", Namespace: "com.example.like"},
			want:     false,
		},
		{
			name:    "wildcard matches with action constraint",
			granted: oauthserver.TestPermission{Resource: "org"},
			required: oauthserver.TestPermission{
				Resource:  "org",
				Namespace: "com.example.type",
				Actions:   []oauthserver.TestScopeAction{oauthserver.ActionCreate},
			},
			want: true,
		},
		{
			name: "granted nil actions satisfies any action requirement",
			granted: oauthserver.TestPermission{
				Resource:  "org",
				Namespace: "com.example.type",
			},
			required: oauthserver.TestPermission{
				Resource:  "org",
				Namespace: "com.example.type",
				Actions:   []oauthserver.TestScopeAction{oauthserver.ActionCreate},
			},
			want: true,
		},
		{
			name: "granted specific action satisfies actionless required",
			granted: oauthserver.TestPermission{
				Resource:  "org",
				Namespace: "com.example.type",
				Actions:   []oauthserver.TestScopeAction{oauthserver.ActionCreate},
			},
			required: oauthserver.TestPermission{
				Resource:  "org",
				Namespace: "com.example.type",
			},
			want: true,
		},
		{
			name: "missing action in granted fails",
			granted: oauthserver.TestPermission{
				Resource:  "org",
				Namespace: "com.example.type",
				Actions:   []oauthserver.TestScopeAction{oauthserver.ActionCreate},
			},
			required: oauthserver.TestPermission{
				Resource:  "org",
				Namespace: "com.example.type",
				Actions:   []oauthserver.TestScopeAction{oauthserver.ActionUpdate},
			},
			want: false,
		},
		{
			name:     "different resource no match",
			granted:  oauthserver.TestPermission{Resource: "repo", Namespace: "com.example.type"},
			required: oauthserver.TestPermission{Resource: "org", Namespace: "com.example.type"},
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := oauthserver.ScopeMatch(tt.granted, tt.required)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestScopesStrategy(t *testing.T) {
	t.Run("wildcard satisfies single", func(t *testing.T) {
		ok := oauthserver.ScopeStrategy([]string{"org:*"}, "org:com.example.type")
		require.True(t, ok)
	})
	t.Run("exact match", func(t *testing.T) {
		ok := oauthserver.ScopeStrategy([]string{"org:com.example.type"}, "org:com.example.type")
		require.True(t, ok)
	})
	t.Run("missing scope", func(t *testing.T) {
		ok := oauthserver.ScopeStrategy(
			[]string{"org:com.example.otherType"},
			"org:com.example.type",
		)
		require.False(t, ok)
	})
	t.Run("empty granted not satisfied", func(t *testing.T) {
		ok := oauthserver.ScopeStrategy([]string{}, "org:com.example.type")
		require.False(t, ok)
	})
	t.Run("needle in multi-item haystack", func(t *testing.T) {
		ok := oauthserver.ScopeStrategy(
			[]string{"org:com.example.otherType", "org:com.example.type"},
			"org:com.example.type",
		)
		require.True(t, ok)
	})
}
