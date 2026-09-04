package pearserver

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseHandle(t *testing.T) {
	tests := []struct {
		name      string
		handleStr string
		want      bool
	}{
		{"empty string", "", false},
		{"invalid handle syntax", "not a valid handle!", false},
		{"single label with no domain", "acme", false},
		{"handle with a dot", "acme.test", false},
		{"handle with a subdomain", "sub.acme.test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			got := parseHandle(t.Context(), w, tt.handleStr)
			require.Equal(t, tt.want, got)
			if !tt.want {
				require.Equal(t, 400, w.Code)
			}
		})
	}
}
