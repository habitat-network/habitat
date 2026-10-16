package mcpserver

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTextOverrides(t *testing.T) {
	text := func(s string) *string { return &s }
	tests := []struct {
		name    string
		header  string
		want    TextOverrides
		wantErr bool
	}{
		{name: "empty header is no overrides", header: "", want: TextOverrides{}},
		{
			name: "both fields",
			header: mustEncode(
				t,
				TextOverrides{Instructions: text("i"), SearchDescription: text("d")},
			),
			want: TextOverrides{Instructions: text("i"), SearchDescription: text("d")},
		},
		{
			name:   "empty text is kept, to send none",
			header: mustEncode(t, TextOverrides{Instructions: text("")}),
			want:   TextOverrides{Instructions: text("")},
		},
		{name: "not base64", header: "%%%", wantErr: true},
		{
			name:    "not json",
			header:  base64.StdEncoding.EncodeToString([]byte("{")),
			wantErr: true,
		},
		{
			name:    "unknown field",
			header:  base64.StdEncoding.EncodeToString([]byte(`{"instruction":"typo"}`)),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTextOverrides(tt.header)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func mustEncode(t *testing.T, o TextOverrides) string {
	t.Helper()
	h, err := o.EncodeHeader()
	require.NoError(t, err)
	return h
}
