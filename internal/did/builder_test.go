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

func TestBuilder_HabitatKey(t *testing.T) {
	ident := New(syntax.DID("did:web:alice.example.com")).
		HabitatKey("zhabitatkey").
		Build()

	require.Equal(t, syntax.DID("did:web:alice.example.com"), ident.DID)
	require.Equal(t, map[string]identity.VerificationMethod{
		"habitat": {
			Type:               "Multikey",
			PublicKeyMultibase: "zhabitatkey",
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

// TestBuilder_Syncer covers the syncer service a space syncer publishes, which
// unlike an account-backed identity declares no keys.
func TestBuilder_Syncer(t *testing.T) {
	ident := New(syntax.DID("did:web:sap.example.com")).
		Syncer("habitat_space_syncer", "https://sap.example.com").
		Build()

	require.Equal(t, syntax.DID("did:web:sap.example.com"), ident.DID)
	require.Equal(t, map[string]identity.ServiceEndpoint{
		"habitat_space_syncer": {
			Type: "HabitatSpaceSyncer",
			URL:  "https://sap.example.com",
		},
	}, ident.Services)
	require.Empty(t, ident.Keys, "a syncer has no keys to publish")
}

func TestBuilder_WebEncodesPort(t *testing.T) {
	// Ports are percent-encoded in the DID but not the URL, per the did:web spec.
	ident := Web("sap.example.com:8443").Build()
	require.Equal(t, syntax.DID("did:web:sap.example.com%3A8443"), ident.DID)
}

func TestBuilder_Custom(t *testing.T) {
	ident := New(syntax.DID("did:web:alice.example.com")).
		AlsoKnownAs("at://alice.example.com").
		VerificationMethod("custom", "CustomType", "zcust").
		Service("custom", "CustomServer", "https://custom.example.com").
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
	require.Equal(t, []string{"at://alice.example.com"}, ident.AlsoKnownAs)
}

func TestBuilder_Web(t *testing.T) {
	require.Equal(t, syntax.DID("did:web:example.com"), Web("example.com").Build().DID)
	require.Equal(t, syntax.DID("did:web:example.com%3A8443"), Web("example.com:8443").Build().DID)
}
