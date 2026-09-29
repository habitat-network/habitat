package utils

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/did"
)

func testPublicKeyMultibase(t *testing.T) string {
	t.Helper()
	priv, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)
	pub, err := priv.PublicKey()
	require.NoError(t, err)
	return pub.Multibase()
}

func TestSupportsSpaces_AdvertisedServiceOrKey(t *testing.T) {
	// These identities advertise support directly in their DID document, so
	// SupportsSpaces must not need to reach their PDS at all: give it a nil
	// client to prove the network path is never taken.
	t.Run("advertises an atproto_space_host service", func(t *testing.T) {
		ident := did.New(syntax.DID("did:web:alice.example.com")).
			ATProtoSpaceHost("https://space-host.example.com").
			Build()
		require.True(t, SupportsSpaces(t.Context(), nil, ident))
	})

	t.Run("advertises an atproto_space verification key", func(t *testing.T) {
		ident := did.New(syntax.DID("did:web:alice.example.com")).
			ATProtoSpaceKey(testPublicKeyMultibase(t)).
			Build()
		require.True(t, SupportsSpaces(t.Context(), nil, ident))
	})
}

func TestSupportsSpaces_ProbesPDS(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		want    bool
		noRoute bool // simulate a PDS that isn't reachable at all
	}{
		{
			// A real spaces-alpha PDS, called with no params:
			// https://spaces-alpha.host.bsky.network/xrpc/com.atproto.simplespace.getSpace
			name:   "missing required param reports InvalidRequest",
			status: http.StatusBadRequest,
			body: `{"error":"InvalidRequest","message":"Invalid com.atproto.simplespace.` +
				`getSpace params: Missing required key \"space\""}`,
			want: true,
		},
		{
			name:   "recognized method succeeds",
			status: http.StatusOK,
			body:   `{"authority":"did:web:example.com","type":"app.example.thing","skey":"abc"}`,
			want:   true,
		},
		{
			name:   "unimplemented method reports MethodNotImplemented",
			status: http.StatusNotImplemented,
			body:   `{"error":"MethodNotImplemented"}`,
			want:   false,
		},
		{
			name:   "unrelated semantic error is not a spaces signal",
			status: http.StatusBadRequest,
			body:   `{"error":"SpaceNotFound","message":"no such space"}`,
			want:   false,
		},
		{
			name:   "unrecognized method returns a bare 404",
			status: http.StatusNotFound,
			body:   ``,
			want:   false,
		},
		{
			name:   "generic router 404 with an unrelated body",
			status: http.StatusNotFound,
			body:   `not found`,
			want:   false,
		},
		{
			name:    "unreachable host",
			noRoute: true,
			want:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var endpoint string
			if tc.noRoute {
				endpoint = "http://127.0.0.1:1" // nothing listens here
			} else {
				srv := httptest.NewServer(http.HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) {
						require.Equal(t, "/xrpc/com.atproto.simplespace.getSpace", r.URL.Path)
						w.WriteHeader(tc.status)
						_, _ = w.Write([]byte(tc.body))
					},
				))
				defer srv.Close()
				endpoint = srv.URL
			}

			ident := &identity.Identity{
				DID: syntax.DID("did:web:alice.example.com"),
				Services: map[string]identity.ServiceEndpoint{
					"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: endpoint},
				},
			}
			got := SupportsSpaces(t.Context(), http.DefaultClient, ident)
			require.Equal(t, tc.want, got)
		})
	}
}
