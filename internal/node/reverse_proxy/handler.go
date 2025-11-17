package reverse_proxy

import (
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

func getHandlerFromRule(rule *Rule, webBundlePath string) (http.Handler, error) {
	switch rule.Type {

	// The reverse proxy will forward the traffic to another service.
	case ProxyRuleRedirect:
		return getRedirectHandler(rule)

	// The reverse proxy will serve files directly from the host system.
	case ProxyRuleFileServer:
		return getFileServerHandler(rule, WithBasePath(webBundlePath))

	default:
		return nil, fmt.Errorf("unknown rule type %s", rule.Type)
	}
}

func getRedirectHandler(rule *Rule) (http.Handler, error) {
	forwardURL, err := url.Parse(rule.Target)
	if err != nil {
		return nil, err
	}

	target := forwardURL.Host

	return &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http" // forwardURL.Scheme
			req.URL.Host = target

			// TODO implement globs
			req.URL.Path = path.Join(
				forwardURL.Path,
				strings.TrimPrefix(req.URL.Path, rule.Matcher),
			)
		},
		Transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout: 10 * time.Second,
			}).Dial,
		},
		ModifyResponse: func(res *http.Response) error {
			return nil
		},
		ErrorHandler: func(rw http.ResponseWriter, r *http.Request, err error) {
			log.Error().Err(err).Msg("reverse proxy request forwarding error")
			rw.WriteHeader(http.StatusInternalServerError)
			_, _ = rw.Write([]byte(err.Error()))
		},
	}, nil
}

func getFileServerHandler(rule *Rule, options ...Option) (http.Handler, error) {
	opts := &FileServerOptions{}
	for _, o := range options {
		o(opts)
	}

	return &fileServerHandler{
		Prefix:  rule.Matcher,
		Path:    rule.Target,
		options: opts,
	}, nil
}

type fileServerHandler struct {
	Prefix string
	Path   string

	options *FileServerOptions
}

func (h *fileServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Try to remove prefix
	oldPath := r.URL.Path
	r.URL.Path = strings.TrimPrefix(oldPath, h.Prefix)

	if h.options.EmbeddedFS != nil {
		// This path is used when we serve from an embedded filesystem

		http.FileServer(http.FS(h.options.EmbeddedFS)).ServeHTTP(w, r)
	} else {
		// Default path: serve files from a directory.

		path := h.Path

		// If a base path is set, and the path is relative, use that instead
		if h.options.BasePath != "" && !filepath.IsAbs(h.Path) {
			path = filepath.Join(h.options.BasePath, h.Path)
		}

		// Ensure the given path exists.
		if _, err := os.Stat(path); os.IsNotExist(err) {
			http.Error(w, fmt.Sprintf("path %s not found on host system", path), http.StatusInternalServerError)
			return
		}

		http.FileServer(http.Dir(path)).ServeHTTP(w, r)
	}
}

// Options for file server rules

type FileServerOptions struct {
	// Instead of serving files from the host filesytstem, serve files embeded into this binary.
	// Currently, this is used to serve the Habitat frontend from the node binary.
	EmbeddedFS fs.FS

	// If set, all file server rules will be relative to this path
	BasePath string
}

type Option func(*FileServerOptions)

func WithFS(fs fs.FS) Option {
	return func(opts *FileServerOptions) {
		opts.EmbeddedFS = fs
	}
}

func WithBasePath(basePath string) Option {
	return func(opts *FileServerOptions) {
		opts.BasePath = basePath
	}
}
