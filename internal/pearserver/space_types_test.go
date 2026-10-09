package pearserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithComAtprotoSpaceTypes(t *testing.T) {
	serve := func(contentType string, body string) *httptest.ResponseRecorder {
		h := withComAtprotoSpaceTypes(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
		})
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
		return w
	}

	t.Run("renames network.habitat.space defs to com.atproto.space", func(t *testing.T) {
		w := serve("application/json", `{"repos":[{"$type":"network.habitat.space.listRepos#repo",`+
			`"did":"did:plc:a"}],"commit":{"$type": "network.habitat.space.defs#signedCommit"}}`)
		require.Equal(t, http.StatusOK, w.Code)
		require.JSONEq(
			t,
			`{"repos":[{"$type":"com.atproto.space.listRepos#repo","did":"did:plc:a"}],`+
				`"commit":{"$type":"com.atproto.space.defs#signedCommit"}}`,
			w.Body.String(),
		)
	})

	t.Run("leaves record types alone", func(t *testing.T) {
		body := `{"value":{"$type":"network.habitat.space.appAccess"}}`
		require.JSONEq(t, body, serve("application/json", body).Body.String())
	})

	t.Run("passes non-JSON bodies through", func(t *testing.T) {
		body := `"$type":"network.habitat.space.listRepos#repo"`
		require.Equal(t, body, serve("application/vnd.ipld.car", body).Body.String())
	})
}
