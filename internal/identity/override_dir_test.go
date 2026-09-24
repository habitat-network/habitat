package identity

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

// unsupportedPDS is an httptest server that serves 404s, so SupportsSpaces
// reads it as "does not implement the spaces protocol".
func unsupportedPDS(t *testing.T) *httptest.Server {
	t.Helper()
	pds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(pds.Close)
	return pds
}

func TestOverrideDirectoryOverridesPDS(t *testing.T) {
	pds := unsupportedPDS(t)

	base := identity.NewMockDirectory()
	base.Insert(identity.Identity{
		DID:         syntax.DID("did:web:alice.example.com"),
		Handle:      syntax.Handle("alice.example.com"),
		AlsoKnownAs: []string{"at://alice.example.com"},
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: pds.URL},
			"habitat":     {Type: "HabitatServer", URL: "https://other.example.com"},
		},
	})

	dir := NewOverrideDirectory(base, "pear.domain")
	dir.httpClient = pds.Client()

	ident, err := dir.LookupDID(t.Context(), syntax.DID("did:web:alice.example.com"))
	require.NoError(t, err)
	doc := ident.DIDDocument()
	require.Len(t, doc.Service, 1)
	require.Equal(t, "#atproto_pds", doc.Service[0].ID)
	require.Equal(t, "AtprotoPersonalDataServer", doc.Service[0].Type)
	require.Equal(t, "https://pear.domain", doc.Service[0].ServiceEndpoint)

	// The base directory's stored identity is untouched (cache-safety: the
	// override must never mutate what the base directory returns).
	original, err := base.LookupDID(t.Context(), syntax.DID("did:web:alice.example.com"))
	require.NoError(t, err)
	require.Equal(t, pds.URL, original.PDSEndpoint())
}

func TestOverrideDirectoryKeepsRealPDS(t *testing.T) {
	pds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(pds.Close)

	base := identity.NewMockDirectory()
	base.Insert(identity.Identity{
		DID:    syntax.DID("did:web:alice.example.com"),
		Handle: syntax.Handle("alice.example.com"),
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: pds.URL},
			"habitat":     {Type: "HabitatServer", URL: "https://other.example.com"},
		},
	})

	dir := NewOverrideDirectory(base, "pear.domain")
	dir.httpClient = pds.Client()

	ident, err := dir.LookupHandle(t.Context(), syntax.Handle("alice.example.com"))
	require.NoError(t, err)
	require.Equal(t, pds.URL, ident.PDSEndpoint())
	require.Equal(t, "https://other.example.com", ident.GetServiceEndpoint("habitat"))
}

func TestOverrideDirectoryLookupResolvesHandleDID(t *testing.T) {
	base := identity.NewMockDirectory()
	base.Insert(identity.Identity{
		DID:    syntax.DID("did:web:alice.example.com"),
		Handle: syntax.Handle("alice.example.com"),
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: "https://pds.example.com"},
		},
	})
	dir := NewOverrideDirectory(base, "pear.domain")
	dir.httpClient = &http.Client{Transport: noNetworkTransport{}}

	atid, err := syntax.ParseAtIdentifier("alice.example.com")
	require.NoError(t, err)
	ident, err := dir.Lookup(t.Context(), atid)
	require.NoError(t, err)
	require.Equal(t, syntax.DID("did:web:alice.example.com"), ident.DID)
	require.Equal(t, "https://pear.domain", ident.PDSEndpoint())
}

func TestOverrideDirectoryLookupIdentifierDID(t *testing.T) {
	base := identity.NewMockDirectory()
	base.Insert(identity.Identity{
		DID:    syntax.DID("did:web:alice.example.com"),
		Handle: syntax.Handle("alice.example.com"),
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: "https://pds.example.com"},
		},
	})
	dir := NewOverrideDirectory(base, "pear.domain")
	dir.httpClient = &http.Client{Transport: noNetworkTransport{}}

	ident, err := dir.LookupIdentifier(t.Context(), "did:web:alice.example.com")
	require.NoError(t, err)
	require.Equal(t, syntax.DID("did:web:alice.example.com"), ident.DID)
	require.Equal(t, "https://pear.domain", ident.PDSEndpoint())
}

func TestOverrideDirectoryLookupIdentifierUnknownHandle(t *testing.T) {
	dir := NewOverrideDirectory(identity.NewMockDirectory(), "pear.domain")

	_, err := dir.LookupIdentifier(t.Context(), "nobody.example.com")
	require.ErrorIs(t, err, identity.ErrHandleNotFound)
}

func TestOverrideDirectoryLookupIdentifierInvalid(t *testing.T) {
	dir := NewOverrideDirectory(identity.NewMockDirectory(), "pear.domain")

	_, err := dir.LookupIdentifier(t.Context(), "not an identifier")
	require.ErrorIs(t, err, identity.ErrInvalidHandle)
}

func TestOverrideDirectoryLookupEmailMintsAndOverrides(t *testing.T) {
	f := newEmailFixture(t)
	dir := NewOverrideDirectory(identity.NewMockDirectory(), "pear.domain")
	dir.httpClient = &http.Client{Transport: noNetworkTransport{}}
	dir.emailResolver = f.resolver

	ident, err := dir.LookupEmail(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	require.Equal(t, syntax.Handle("alice.acme.example.com"), ident.Handle)
	require.Equal(t, "https://pear.domain", ident.PDSEndpoint())
	// Minting alone must not enroll the identity in the org.
	require.Equal(t, 0, f.memberships(t))
}

func TestOverrideDirectoryLookupIdentifierEmail(t *testing.T) {
	f := newEmailFixture(t)
	dir := NewOverrideDirectory(identity.NewMockDirectory(), "pear.domain")
	dir.httpClient = &http.Client{Transport: noNetworkTransport{}}
	dir.emailResolver = f.resolver

	ident, err := dir.LookupIdentifier(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	require.Equal(t, syntax.Handle("alice.acme.example.com"), ident.Handle)
	require.Equal(t, "https://pear.domain", ident.PDSEndpoint())
}

func TestOverrideDirectoryLookupEmailUnknownDomain(t *testing.T) {
	f := newEmailFixture(t)
	dir := NewOverrideDirectory(identity.NewMockDirectory(), "pear.domain")
	dir.emailResolver = f.resolver

	_, err := dir.LookupEmail(t.Context(), "alice@other.com")
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
}

func TestOverrideDirectoryLookupEmailDisabled(t *testing.T) {
	dir := NewOverrideDirectory(identity.NewMockDirectory(), "pear.domain")

	_, err := dir.LookupEmail(t.Context(), "alice@acme.com")
	require.ErrorIs(t, err, identity.ErrInvalidHandle)
}

func TestOverrideDirectoryPurge(t *testing.T) {
	dir := NewOverrideDirectory(identity.NewMockDirectory(), "pear.domain")

	atid, err := syntax.ParseAtIdentifier("alice.example.com")
	require.NoError(t, err)
	require.NoError(t, dir.Purge(t.Context(), atid))
}

func TestOverrideDirectoryPassesThroughErrors(t *testing.T) {
	dir := NewOverrideDirectory(identity.NewMockDirectory(), "pear.domain")

	_, err := dir.LookupHandle(t.Context(), syntax.Handle("alice.example.com"))
	require.ErrorIs(t, err, identity.ErrHandleNotFound)

	_, err = dir.LookupDID(t.Context(), syntax.DID("did:web:alice.example.com"))
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
}
