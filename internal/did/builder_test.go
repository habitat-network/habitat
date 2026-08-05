package did

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

func TestBuilder_Atproto(t *testing.T) {
	ident := New(syntax.DID("did:web:alice.example.com")).
		AtprotoKey("zpubkey").
		Build()

	require.Equal(t, syntax.DID("did:web:alice.example.com"), ident.DID)
	require.Equal(t, map[string]identity.VerificationMethod{
		"atproto": {
			Type:               "Multikey",
			PublicKeyMultibase: "zpubkey",
		},
	}, ident.Keys)
}

func TestBuilder_Services(t *testing.T) {
	ident := New(syntax.DID("did:web:alice.example.com")).
		Habitat("https://pear.example.com").
		ATProtoPDS("https://pear.example.com").
		Build()

	require.Equal(t, map[string]identity.ServiceEndpoint{
		"habitat": {
			Type: "HabitatServer",
			URL:  "https://pear.example.com",
		},
		"atproto_pds": {
			Type: "AtprotoPersonalDataServer",
			URL:  "https://pear.example.com",
		},
	}, ident.Services)
}

func TestBuilder_Custom(t *testing.T) {
	ident := New(syntax.DID("did:web:alice.example.com")).
		AlsoKnownAs("at://alice.example.com").
		VerificationMethod("did:web:alice.example.com#custom", "CustomType", "zcust").
		Service("#custom", "CustomServer", "https://custom.example.com").
		Build()

	require.Equal(t, []string{"at://alice.example.com"}, ident.AlsoKnownAs)
	require.Equal(t, map[string]identity.VerificationMethod{
		"custom": {
			Type:               "CustomType",
			PublicKeyMultibase: "zcust",
		},
	}, ident.Keys)
	require.Equal(t, map[string]identity.ServiceEndpoint{
		"custom": {
			Type: "CustomServer",
			URL:  "https://custom.example.com",
		},
	}, ident.Services)
}

func TestBuilder_Handle(t *testing.T) {
	ident := New(syntax.DID("did:web:alice.example.com")).
		Handle(syntax.Handle("alice.example.com")).
		Build()

	require.Equal(t, syntax.Handle("alice.example.com"), ident.Handle)
}

func TestBuilder_Web(t *testing.T) {
	require.Equal(t, syntax.DID("did:web:example.com"), Web("example.com").Build().DID)
	require.Equal(t, syntax.DID("did:web:example.com%3A8443"), Web("example.com:8443").Build().DID)
}
