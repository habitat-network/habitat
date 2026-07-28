package identity

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	atpidentity "github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

func testResolveServer() *Server {
	dir := atpidentity.NewMockDirectory()
	dir.Insert(atpidentity.Identity{
		DID:         syntax.DID("did:web:alice.example.com"),
		Handle:      syntax.Handle("alice.example.com"),
		AlsoKnownAs: []string{"at://alice.example.com"},
		Services: map[string]identity.ServiceEndpoint{
			"atproto": {
				Type: "AtprotoPersonalDataServer",
				URL:  "https://public.pds",
			},
		},
	})
	return &Server{directory: dir, domain: "pear.domain"}
}

func TestResolveDID(t *testing.T) {
	s := testResolveServer()
	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/com.atproto.identity.resolveDid?did=did:web:alice.example.com",
		nil,
	)
	w := httptest.NewRecorder()

	s.ResolveDID(w, req)

	require.Equal(t, http.StatusOK, w.Code)
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
		w.Body.String(),
	)
}

func TestResolveHandle(t *testing.T) {
	s := testResolveServer()
	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/com.atproto.identity.resolveHandle?handle=alice.example.com",
		nil,
	)
	w := httptest.NewRecorder()

	s.ResolveHandle(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"did": "did:web:alice.example.com"}`, w.Body.String())
}

func TestResolveIdentity(t *testing.T) {
	s := testResolveServer()
	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/com.atproto.identity.resolveIdentity?identifier=alice.example.com",
		nil,
	)
	w := httptest.NewRecorder()

	s.ResolveIdentity(w, req)

	require.Equal(t, http.StatusOK, w.Code)
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
		w.Body.String(),
	)
}

func TestResolveHandleNotFound(t *testing.T) {
	s := testResolveServer()
	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/com.atproto.identity.resolveHandle?handle=nobody.example.com",
		nil,
	)
	w := httptest.NewRecorder()

	s.ResolveHandle(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	require.JSONEq(t, `{"error": "HandleNotFound", "message": "handle not found"}`, w.Body.String())
}
