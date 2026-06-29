package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func newStaticHandler() *staticHandler {
	return &staticHandler{files: fstest.MapFS{
		"index.html":             {Data: []byte("root")},
		"assets/app.js":          {Data: []byte("js")},
		"admin/login/index.html": {Data: []byte("admin login")},
		"login/habitat.html":     {Data: []byte("member login")},
	}}
}

func TestStaticHandlerServesPaths(t *testing.T) {
	h := newStaticHandler()

	cases := []struct {
		name string
		path string
		body string
	}{
		{"root", "/ui/", "root"},
		{"asset", "/ui/assets/app.js", "js"},
		{"directory index", "/ui/admin/login", "admin login"},
		{"html suffix", "/ui/login/habitat", "member login"},
		// Unknown deep links fall back to the root document (SPA behaviour).
		{"spa fallback", "/ui/does/not/exist", "root"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, tc.body, rec.Body.String())
		})
	}
}

func TestStaticHandlerNotFoundWithoutFallback(t *testing.T) {
	h := &staticHandler{files: fstest.MapFS{"assets/app.js": {Data: []byte("js")}}}

	req := httptest.NewRequest(http.MethodGet, "/ui/missing", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestNewReverseProxiesInDev(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/ui/login/habitat", r.URL.Path)
		_, _ = w.Write([]byte("from dev server"))
	}))
	defer upstream.Close()

	h, err := New(upstream.URL)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/ui/login/habitat", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "from dev server", rec.Body.String())
}

func TestNewServesEmbeddedWhenNoDevProxy(t *testing.T) {
	h, err := New("")
	require.NoError(t, err)
	require.NotNil(t, h)
}

func TestNewRejectsInvalidProxyTarget(t *testing.T) {
	_, err := New("://not a url")
	require.Error(t, err)
}
