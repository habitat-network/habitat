// Package webui serves the pre-rendered pear-pages UI (member login and
// instance admin pages) that pear embeds and serves under the /ui/ path.
//
// The static build is produced by the pear-pages app and copied into ./dist by
// `pear-pages:build`, which `pear:build` depends on. During development a
// reverse proxy to the pear-pages dev server is used instead (see New) so UI
// changes are reflected without re-embedding.
package webui

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
)

// dist holds the pre-rendered static site. The committed .gitkeep keeps this
// embeddable before the UI has been built; `pear-pages:build` overwrites it
// with the real output.
//
//go:embed all:dist
var dist embed.FS

// New returns a handler that serves the embedded pear-pages build. It is meant
// to be mounted at the /ui/ path prefix.
//
// If devProxyTarget is non-empty, requests are reverse-proxied to that URL (the
// pear-pages dev server) instead of being served from the embedded build.
func New(devProxyTarget string) (http.Handler, error) {
	if devProxyTarget != "" {
		target, err := url.Parse(devProxyTarget)
		if err != nil {
			return nil, fmt.Errorf("parsing ui dev proxy target %q: %w", devProxyTarget, err)
		}
		return httputil.NewSingleHostReverseProxy(target), nil
	}

	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, fmt.Errorf("opening embedded ui dist: %w", err)
	}
	return &staticHandler{files: sub}, nil
}

// staticHandler serves files from the pre-rendered build, resolving the /ui/
// mount prefix and the various ways a pre-rendered route may be written to disk
// (e.g. "admin/login", "admin/login.html", or "admin/login/index.html").
type staticHandler struct {
	files fs.FS
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rel := strings.Trim(strings.TrimPrefix(r.URL.Path, "/ui"), "/")

	var candidates []string
	if rel != "" {
		candidates = append(candidates, rel, rel+".html", path.Join(rel, "index.html"))
	}
	// SPA-style fallback so deep links to pre-rendered routes still resolve.
	candidates = append(candidates, "index.html")

	for _, name := range candidates {
		if info, err := fs.Stat(h.files, name); err == nil && !info.IsDir() {
			http.ServeFileFS(w, r, h.files, name)
			return
		}
	}
	http.NotFound(w, r)
}
