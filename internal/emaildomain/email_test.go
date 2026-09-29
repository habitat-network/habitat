package emaildomain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/emaildomain"
)

func TestParseEmail(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    emaildomain.Email
		wantErr bool
	}{
		{name: "plain", in: "alice@acme.com", want: "alice@acme.com"},
		{name: "lowercases", in: "Alice@ACME.com", want: "alice@acme.com"},
		{name: "subdomain", in: "bob@eng.acme.co.uk", want: "bob@eng.acme.co.uk"},
		{name: "display name", in: "Alice <alice@acme.com>", wantErr: true},
		{name: "no local part", in: "@acme.com", wantErr: true},
		{name: "no domain", in: "alice@", wantErr: true},
		{name: "single-label domain", in: "alice@localhost", wantErr: true},
		{name: "handle", in: "alice.acme.com", wantErr: true},
		{name: "did", in: "did:web:acme.com", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := emaildomain.ParseEmail(tt.in)
			if tt.wantErr {
				require.ErrorIs(t, err, emaildomain.ErrInvalidEmail)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEmailParts(t *testing.T) {
	email, err := emaildomain.ParseEmail("alice@acme.com")
	require.NoError(t, err)
	require.Equal(t, "alice", email.LocalPart())
	require.Equal(t, "acme.com", email.Domain())
}
