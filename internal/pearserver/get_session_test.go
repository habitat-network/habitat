package pearserver_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/forwarding"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/pdsclient"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
)

func TestServer_GetSession(t *testing.T) {
	t.Run("returns the session for a hive-hosted identity", func(t *testing.T) {
		db := db_testutil.NewDB(t)
		h, err := hive.NewHive("example.com", "pear.example.com", db)
		require.NoError(t, err)
		ident, err := h.MintIdentity(t.Context(), "alice", "org")
		require.NoError(t, err)

		ts := pearserver_testutil.NewTestServer(t,
			pearserver_testutil.WithDB(db),
			pearserver_testutil.WithHive(h),
			pearserver_testutil.WithValidator(authntest.NewSuccessValidator(
				&authn.CredentialInfo{Subject: ident.DID},
			)),
		)

		req := httptest.NewRequest(
			http.MethodGet,
			"/xrpc/com.atproto.server.getSession",
			http.NoBody,
		)
		w := httptest.NewRecorder()
		ts.Server.GetSession(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var out atproto.ServerGetSession_Output
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		require.Equal(t, ident.DID.String(), out.Did)
		require.Equal(t, ident.Handle.String(), out.Handle)
	})

	t.Run("returns 401 for an unauthenticated request", func(t *testing.T) {
		ts := pearserver_testutil.NewTestServer(t,
			pearserver_testutil.WithValidator(authntest.NewFailureValidator()),
		)

		req := httptest.NewRequest(
			http.MethodGet,
			"/xrpc/com.atproto.server.getSession",
			http.NoBody,
		)
		w := httptest.NewRecorder()
		ts.Server.GetSession(w, req)

		require.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("forwards remote identities to their real PDS", func(t *testing.T) {
		const remoteDIDStr = "did:plc:remote"
		fakePDS := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/xrpc/com.atproto.server.getSession", r.URL.Path)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(
					[]byte(`{"did":"` + remoteDIDStr + `","handle":"carol.example.com"}`),
				)
			}),
		)
		t.Cleanup(fakePDS.Close)
		remoteDID := syntax.DID(remoteDIDStr)

		fwd := forwarding.NewPDSForwarding(
			nil,
			authntest.NewSuccessValidator(&authn.CredentialInfo{Subject: remoteDID}),
			pdsclient.NewDummyClientFactory(fakePDS.URL),
			pdsclient.NewDummyDirectory(fakePDS.URL),
		)
		ts := pearserver_testutil.NewTestServer(t,
			pearserver_testutil.WithValidator(authntest.NewSuccessValidator(
				&authn.CredentialInfo{Subject: remoteDID},
			)),
			pearserver_testutil.WithPDSForwarding(fwd),
		)

		req := httptest.NewRequest(
			http.MethodGet,
			"/xrpc/com.atproto.server.getSession",
			http.NoBody,
		)
		w := httptest.NewRecorder()
		ts.Server.GetSession(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		require.JSONEq(
			t,
			`{"did":"`+remoteDIDStr+`","handle":"carol.example.com"}`,
			w.Body.String(),
		)
	})
}
