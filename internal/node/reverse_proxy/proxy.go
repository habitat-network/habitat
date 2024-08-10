package reverse_proxy

import (
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/rs/zerolog/log"
)

type RuleSet struct {
	rules        map[string]RuleHandler
	baseFilePath string // Optional, if set, all file server rules will be relative to this path
}

func (rs RuleSet) Add(name string, rule RuleHandler) error {
	if _, ok := rs.rules[name]; ok {
		return fmt.Errorf("rule name %s is already taken", name)
	}
	rs.rules[name] = rule
	return nil
}

// AddRule is a wrapper around Add for finding the correct rule handler type.
func (rs RuleSet) AddRule(rule *node.ReverseProxyRule) error {
	if rule.Type == ProxyRuleRedirect {
		url, err := url.Parse(rule.Target)
		if err != nil {
			return err
		}
		err = rs.Add(rule.ID, &RedirectRule{
			Matcher:         rule.Matcher,
			ForwardLocation: url,
		})
		if err != nil {
			return err
		}
	} else if rule.Type == ProxyRuleFileServer {
		err := rs.Add(rule.ID, &FileServerRule{
			Matcher:  rule.Matcher,
			Path:     rule.Target,
			BasePath: rs.baseFilePath,
		})
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("unknown rule type %s", rule.Type)
	}
	return nil
}

func (rs RuleSet) Remove(name string) error {
	if _, ok := rs.rules[name]; !ok {
		return fmt.Errorf("rule %s does not exist", name)
	}
	delete(rs.rules, name)
	return nil
}

type RuleHandler interface {
	Type() string
	Match(url *url.URL) bool
	Handler() http.Handler
	Rank() int
}

type FileServerRule struct {
	Matcher string
	Path    string

	FS       fs.FS  // Optional, instead of using Path, pass in an fs.FS. Useful for embedding the Habitat frontend.
	BasePath string // Optional, if set, all file server rules will be relative to this path
}

func (r *FileServerRule) Type() string {
	return ProxyRuleFileServer
}

func (r *FileServerRule) Match(url *url.URL) bool {
	// TODO make this work with actual glob strings
	// For now, just match based off of base path
	return strings.HasPrefix(url.Path, r.Matcher)
}

func (r *FileServerRule) Handler() http.Handler {
	return &FileServerHandler{
		Prefix:   r.Matcher,
		Path:     r.Path,
		FS:       r.FS,
		BasePath: r.BasePath,
	}
}

func (r *FileServerRule) Rank() int {
	return strings.Count(r.Matcher, "/")
}

type FileServerHandler struct {
	Prefix string
	Path   string

	BasePath string // Optional, if set, all file server rules will be relative to this path
	FS       fs.FS  // Optional, instead of using Path, pass in an fs.FS. Useful for embedding the Habitat frontend.
}

func (h *FileServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Try to remove prefix
	oldPath := r.URL.Path
	r.URL.Path = strings.TrimPrefix(oldPath, h.Prefix)

	if oldPath == r.URL.Path {
		// Something weird happened
		_, _ = w.Write([]byte("unable to remove url path prefix"))
		w.WriteHeader(http.StatusInternalServerError)
	}

	if h.FS != nil {
		http.FileServer(http.FS(h.FS)).ServeHTTP(w, r)
	} else {
		path := h.Path

		// If a base path is set, and the path is relative, use that instead
		if h.BasePath != "" && !filepath.IsAbs(h.Path) {
			path = filepath.Join(h.BasePath, h.Path)
		}
		http.FileServer(http.Dir(path)).ServeHTTP(w, r)
	}
}

type RedirectRule struct {
	Matcher         string
	ForwardLocation *url.URL
}

func (r *RedirectRule) Type() string {
	return ProxyRuleRedirect
}

func (r *RedirectRule) Match(url *url.URL) bool {
	// TODO make this work with actual glob strings
	// For now, just match based off of base path
	return strings.HasPrefix(url.Path, r.Matcher)
}

func (r *RedirectRule) Handler() http.Handler {
	target := r.ForwardLocation.Host

	return &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = r.ForwardLocation.Scheme
			req.URL.Host = target
			// TODO implement globs
			req.URL.Path = path.Join(r.ForwardLocation.Path, strings.TrimPrefix(req.URL.Path, r.Matcher))
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
			_, _ = rw.Write([]byte(err.Error()))
			rw.WriteHeader(http.StatusInternalServerError)
		},
	}
}

func (r *RedirectRule) Rank() int {
	return strings.Count(r.Matcher, "/")
}
