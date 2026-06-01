package oauthserver

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPermissionFromScope(t *testing.T) {
	tests := []struct {
		name    string
		scope   string
		want    permission
		wantErr bool
	}{
		{
			name:  "all spaces types wildcard",
			scope: "org:*",
			want:  permission{Resource: "org", Namespace: "*", Actions: nil},
		},
		{
			name:  "single space type",
			scope: "org:com.example.type",
			want:  permission{Resource: "org", Namespace: "com.example.type", Actions: nil},
		},
		{
			name:  "single space with actions",
			scope: "org:com.example.type?action=create&action=update",
			want: permission{
				Resource:  "org",
				Namespace: "com.example.type",
				Actions:   []string{"create", "update"},
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

func TestScopeMatch(t *testing.T) {
	tests := []struct {
		name     string
		granted  permission
		required permission
		want     bool
	}{
		{
			name:     "wildcard matches any space type",
			granted:  permission{Resource: "org", Namespace: "*"},
			required: permission{Resource: "org", Namespace: "com.example.type"},
			want:     true,
		},
		{
			name:     "exact match",
			granted:  permission{Resource: "org", Namespace: "com.example.type"},
			required: permission{Resource: "org", Namespace: "com.example.type"},
			want:     true,
		},
		{
			name:     "different collection no match",
			granted:  permission{Resource: "org", Namespace: "com.example.type"},
			required: permission{Resource: "org", Namespace: "com.example.like"},
			want:     false,
		},
		{
			name:    "wildcard matches with action constraint",
			granted: permission{Resource: "org", Namespace: "*"},
			required: permission{
				Resource:  "org",
				Namespace: "com.example.type",
				Actions:   []string{"create"},
			},
			want: true,
		},
		{
			name: "granted nil actions satisfies any action requirement",
			granted: permission{
				Resource:  "org",
				Namespace: "com.example.type",
			},
			required: permission{
				Resource:  "org",
				Namespace: "com.example.type",
				Actions:   []string{"create"},
			},
			want: true,
		},
		{
			name: "granted specific action satisfies actionless required",
			granted: permission{
				Resource:  "org",
				Namespace: "com.example.type",
				Actions:   []string{"create"},
			},
			required: permission{
				Resource:  "org",
				Namespace: "com.example.type",
			},
			want: true,
		},
		{
			name: "missing action in granted fails",
			granted: permission{
				Resource:  "org",
				Namespace: "com.example.type",
				Actions:   []string{"create"},
			},
			required: permission{
				Resource:  "org",
				Namespace: "com.example.type",
				Actions:   []string{"delete"},
			},
			want: false,
		},
		{
			name:     "different resource no match",
			granted:  permission{Resource: "repo", Namespace: "com.example.type"},
			required: permission{Resource: "org", Namespace: "com.example.type"},
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scopeMatch(tt.granted, tt.required)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestScopesStrategy(t *testing.T) {
	t.Run("wildcard satisfies single", func(t *testing.T) {
		ok := scopeStrategy([]string{"org:com.example.type"}, "org:*")
		require.True(t, ok)
	})
	t.Run("exact match", func(t *testing.T) {
		ok := scopeStrategy([]string{"org:com.example.type"}, "org:com.example.type")
		require.True(t, ok)
	})
	t.Run("missing scope", func(t *testing.T) {
		ok := scopeStrategy([]string{"org:com.example.otherType"}, "org:com.example.type")
		require.False(t, ok)
	})
	t.Run("empty required always satisfied", func(t *testing.T) {
		ok := scopeStrategy([]string{}, "org:com.example.type")
		require.True(t, ok)
	})
	t.Run("multiple required one missing", func(t *testing.T) {
		ok := scopeStrategy(
			[]string{"org:com.example.type", "org:com.example.otherType"},
			"org:com.example.type",
		)
		require.False(t, ok)
	})
}
