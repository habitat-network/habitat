package reverse_proxy

import (
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/rs/zerolog/log"
)

type RuleSet map[string]RuleHandler

func (r RuleSet) Add(name string, rule RuleHandler) error {
	if _, ok := r[name]; ok {
		return fmt.Errorf("rule name %s is already taken", name)
	}
	r[name] = rule
	return nil
}

func (r RuleSet) AddRule(rule *node.ReverseProxyRule) error {
	if rule.Type == ProxyRuleRedirect {
		url, err := url.Parse(rule.Target)
		if err != nil {
			return err
		}
		err = r.Add(rule.ID, &RedirectRule{
			Matcher:         rule.Matcher,
			ForwardLocation: url,
		})
		if err != nil {
			return err
		}
	} else if rule.Type == ProxyRuleFileServer {
		err := r.Add(rule.ID, &FileServerRule{
			Matcher: rule.Matcher,
			Path:    rule.Target,
		})
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("unknown rule type %s", rule.Type)
	}
	return nil
}

func (r RuleSet) Remove(name string) error {
	if _, ok := r[name]; !ok {
		return fmt.Errorf("rule %s does not exist", name)
	}
	delete(r, name)
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
	FS      fs.FS // Optional, instead of using Path, pass in an fs.FS. Useful for embedding the Habitat frontend.
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
		Prefix: r.Matcher,
		Path:   r.Path,
		FS:     r.FS,
	}
}

func (r *FileServerRule) Rank() int {
	return strings.Count(r.Matcher, "/")
}

type FileServerHandler struct {
	Prefix string
	Path   string

	FS fs.FS // Optional, instead of using Path, pass in an fs.FS. Useful for embedding the Habitat frontend.
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
		http.FileServer(http.Dir(h.Path)).ServeHTTP(w, r)
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
