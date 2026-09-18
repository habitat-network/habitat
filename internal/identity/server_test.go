package identity

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
)

// testResolveServer builds a Server resolving a single mock identity whose
// "PDS" is a local test server that doesn't implement the spaces protocol (a
// bare 404), so overriddenDidDoc always takes the "redirect to this habitat
// instance" branch these tests assert on.
func testResolveServer(t *testing.T) *Server {
	t.Helper()
	pds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(pds.Close)

	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID:         syntax.DID("did:web:alice.example.com"),
		Handle:      syntax.Handle("alice.example.com"),
		AlsoKnownAs: []string{"at://alice.example.com"},
		Services: map[string]identity.ServiceEndpoint{
			"atproto": {
				Type: "AtprotoPersonalDataServer",
				URL:  pds.URL,
			},
		},
	})
	return &Server{directory: dir, domain: "pear.domain", httpClient: pds.Client()}
}

func TestResolveDID(t *testing.T) {
	s := testResolveServer(t)

	var out json.RawMessage
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.ResolveDID,
		url.Values{"did": []string{"did:web:alice.example.com"}},
		&out,
	)

	require.Equal(t, http.StatusOK, code)
	require.JSONEq(
		t,
		`{
			"didDoc": {
				"id": "did:web:alice.example.com",
				"alsoKnownAs": ["at://alice.example.com"],
				"service": [
					{
						"id": "#atproto_pds",
						"type": "AtprotoPersonalDataServer",
						"serviceEndpoint": "https://pear.domain"
					}
				]
			}
		}`,
		string(out),
	)
}

func TestResolveHandle(t *testing.T) {
	s := testResolveServer(t)

	var out json.RawMessage
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.ResolveHandle,
		url.Values{"handle": []string{"alice.example.com"}},
		&out,
	)

	require.Equal(t, http.StatusOK, code)
	require.JSONEq(t, `{"did": "did:web:alice.example.com"}`, string(out))
}

func TestResolveIdentity(t *testing.T) {
	s := testResolveServer(t)

	var out json.RawMessage
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.ResolveIdentity,
		url.Values{"identifier": []string{"alice.example.com"}},
		&out,
	)

	require.Equal(t, http.StatusOK, code)
	require.JSONEq(
		t,
		`{
			"did": "did:web:alice.example.com",
			"handle": "alice.example.com",
			"didDoc": {
				"id": "did:web:alice.example.com",
				"alsoKnownAs": ["at://alice.example.com"],
				"service": [
					{
						"id": "#atproto_pds",
						"type": "AtprotoPersonalDataServer",
						"serviceEndpoint": "https://pear.domain"
					}
				]
			}
		}`,
		string(out),
	)
}

func TestResolveHandleNotFound(t *testing.T) {
	s := testResolveServer(t)

	var out json.RawMessage
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.ResolveHandle,
		url.Values{"handle": []string{"nobody.example.com"}},
		&out,
	)

	require.Equal(t, http.StatusNotFound, code)
	require.JSONEq(t, `{"error": "HandleNotFound", "message": "handle not found"}`, string(out))
}
