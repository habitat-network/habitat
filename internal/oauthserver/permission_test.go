package oauthserver

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPermissionFromScope(t *testing.T) {
	tests := []struct {
		name    string
		scope   string
		want    Permission
		wantErr bool
	}{
		{
			name:  "all collections wildcard",
			scope: "org:*",
			want:  Permission{Resource: "org", Collection: "*", Actions: nil},
		},
		{
			name:  "single collection",
			scope: "org:com.example.post",
			want:  Permission{Resource: "org", Collection: "com.example.post", Actions: nil},
		},
		{
			name:  "single collection with actions",
			scope: "org:com.example.post?action=create&action=update",
			want:  Permission{Resource: "org", Collection: "com.example.post", Actions: []string{"create", "update"}},
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
			got, err := PermissionFromScope(tt.scope)
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
		granted  string
		required string
		want     bool
	}{
		{
			name:     "wildcard matches any collection",
			granted:  "org:*",
			required: "org:com.example.post",
			want:     true,
		},
		{
			name:     "exact match",
			granted:  "org:com.example.post",
			required: "org:com.example.post",
			want:     true,
		},
		{
			name:     "different collection no match",
			granted:  "org:com.example.post",
			required: "org:com.example.like",
			want:     false,
		},
		{
			name:     "wildcard matches with action constraint",
			granted:  "org:*",
			required: "org:com.example.post?action=create",
			want:     true,
		},
		{
			name:     "granted nil actions satisfies any action requirement",
			granted:  "org:com.example.post",
			required: "org:com.example.post?action=create",
			want:     true,
		},
		{
			name:     "granted specific action satisfies actionless required",
			granted:  "org:com.example.post?action=create",
			required: "org:com.example.post",
			want:     true,
		},
		{
			name:     "missing action in granted fails",
			granted:  "org:com.example.post?action=create",
			required: "org:com.example.post?action=delete",
			want:     false,
		},
		{
			name:     "different resource no match",
			granted:  "repo:com.example.post",
			required: "org:com.example.post",
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grantedP, err := PermissionFromScope(tt.granted)
			require.NoError(t, err)
			requiredP, err := PermissionFromScope(tt.required)
			require.NoError(t, err)
			got := ScopeMatch(grantedP, requiredP)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestScopesSatisfy(t *testing.T) {
	t.Run("wildcard satisfies single", func(t *testing.T) {
		ok := ScopesSatisfy([]string{"org:*"}, []string{"org:com.example.post"})
		require.True(t, ok)
	})
	t.Run("exact match", func(t *testing.T) {
		ok := ScopesSatisfy([]string{"org:com.example.post"}, []string{"org:com.example.post"})
		require.True(t, ok)
	})
	t.Run("missing scope", func(t *testing.T) {
		ok := ScopesSatisfy([]string{"org:com.example.post"}, []string{"org:com.example.like"})
		require.False(t, ok)
	})
	t.Run("empty required always satisfied", func(t *testing.T) {
		ok := ScopesSatisfy([]string{"org:com.example.post"}, nil)
		require.True(t, ok)
	})
	t.Run("multiple required one missing", func(t *testing.T) {
		ok := ScopesSatisfy(
			[]string{"org:com.example.post"},
			[]string{"org:com.example.post", "org:com.example.like"},
		)
		require.False(t, ok)
	})
}
