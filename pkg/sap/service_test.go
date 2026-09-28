package sap

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewServiceIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		endpoint    string
		serviceName string
		wantRef     string
	}{
		{
			name:        "default service name",
			endpoint:    "https://sap.example.com",
			serviceName: "",
			wantRef:     "did:web:sap.example.com#" + DefaultServiceName,
		},
		{
			name:        "explicit service name",
			endpoint:    "https://sap.example.com",
			serviceName: "atproto_space_syncer",
			wantRef:     "did:web:sap.example.com#atproto_space_syncer",
		},
		{
			name:        "default domain is used verbatim",
			endpoint:    "https://sap.local.habitat.network",
			serviceName: "",
			wantRef:     "did:web:sap.local.habitat.network#" + DefaultServiceName,
		},
		{
			name:        "trailing slash does not leak into the did",
			endpoint:    "https://sap.example.com/",
			serviceName: "",
			wantRef:     "did:web:sap.example.com#" + DefaultServiceName,
		},
		{
			name:        "http endpoint",
			endpoint:    "http://localhost",
			serviceName: "",
			wantRef:     "did:web:localhost#" + DefaultServiceName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			identity, err := NewServiceIdentity(tt.endpoint, tt.serviceName)
			require.NoError(t, err)
			require.Equal(t, tt.wantRef, identity.Ref())
			// The DID is the identifier's DID half, which is also what the DID
			// document is served under.
			wantDID := strings.TrimSuffix(tt.wantRef, "#"+identity.Name)
			require.Equal(t, wantDID, identity.DID.String())
		})
	}
}

func TestNewServiceIdentityRejectsUnusableInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		endpoint    string
		serviceName string
	}{
		{"service name with fragment separator", "https://sap.example.com", "a#b"},
		{"service name with space", "https://sap.example.com", "a b"},
		{"relative endpoint", "/notify", ""},
		{"endpoint without scheme", "sap.example.com", ""},
		{"endpoint with unsupported scheme", "ftp://sap.example.com", ""},
		{"empty endpoint", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewServiceIdentity(tt.endpoint, tt.serviceName)
			require.Error(t, err)
		})
	}
}
