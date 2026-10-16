package pearserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

// Handles must expose the inner route's template to middleware on the parent
// router, so otel can name spans by route.
func TestHandlesSetsPattern(t *testing.T) {
	ps := &PearServer{router: mux.NewRouter()}
	ps.router.HandleFunc(
		"/xrpc/network.habitat.space.listSpaces",
		func(http.ResponseWriter, *http.Request) {},
	)

	var got string
	parent := mux.NewRouter()
	parent.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.Pattern
			next.ServeHTTP(w, r)
		})
	})
	parent.MatcherFunc(ps.Handles).Handler(ps)

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.space.listSpaces", nil)
	parent.ServeHTTP(httptest.NewRecorder(), req)
	require.Equal(t, "/xrpc/network.habitat.space.listSpaces", got)
}
