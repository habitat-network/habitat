package did

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

func TestBuilder_Atproto(t *testing.T) {
	doc := New(syntax.DID("did:web:alice.example.com")).
		AtprotoKey("zpubkey").
		Build()

	require.Equal(t, syntax.DID("did:web:alice.example.com"), doc.DID)
	require.Equal(t, []identity.DocVerificationMethod{{
		ID:                 "did:web:alice.example.com#atproto",
		Type:               "Multikey",
		Controller:         "did:web:alice.example.com",
		PublicKeyMultibase: "zpubkey",
	}}, doc.VerificationMethod)
}

func TestBuilder_Services(t *testing.T) {
	doc := New(syntax.DID("did:web:alice.example.com")).
		Habitat("https://pear.example.com").
		ATProtoPDS("https://pear.example.com").
		Build()

	require.Equal(t, []identity.DocService{
		{ID: "#habitat", Type: "HabitatServer", ServiceEndpoint: "https://pear.example.com"},
		{
			ID:              "#atproto_pds",
			Type:            "AtprotoPersonalDataServer",
			ServiceEndpoint: "https://pear.example.com",
		},
	}, doc.Service)
}

func TestBuilder_Custom(t *testing.T) {
	doc := New(syntax.DID("did:web:alice.example.com")).
		AlsoKnownAs("at://alice.example.com").
		VerificationMethod("did:web:alice.example.com#custom", "CustomType", "did:web:alice.example.com", "zcust").
		Service("#custom", "CustomServer", "https://custom.example.com").
		Build()

	require.Equal(t, []string{"at://alice.example.com"}, doc.AlsoKnownAs)
	require.Equal(t, []identity.DocVerificationMethod{{
		ID:                 "did:web:alice.example.com#custom",
		Type:               "CustomType",
		Controller:         "did:web:alice.example.com",
		PublicKeyMultibase: "zcust",
	}}, doc.VerificationMethod)
	require.Equal(t, []identity.DocService{{
		ID:              "#custom",
		Type:            "CustomServer",
		ServiceEndpoint: "https://custom.example.com",
	}}, doc.Service)
}

func TestBuilder_Web(t *testing.T) {
	require.Equal(t, syntax.DID("did:web:example.com"), Web("example.com").Build().DID)
	require.Equal(t, syntax.DID("did:web:example.com%3A8443"), Web("example.com:8443").Build().DID)
}
