package reverse_proxy

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"tailscale.com/tsnet"
)

type ProxyServer struct {
	logger           *zerolog.Logger
	RuleSet          *RuleSet
	defaultServePath string
}

func NewProxyServer(logger *zerolog.Logger, defaultServePath string) *ProxyServer {
	return &ProxyServer{
		logger: logger,
		RuleSet: &RuleSet{
			rules:        make(map[string]*node.ReverseProxyRule),
			baseFilePath: defaultServePath,
		},
		defaultServePath: defaultServePath,
	}
}

func (s *ProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var bestMatch *node.ReverseProxyRule = nil
	// Find the matching rule with the highest "rank", aka the most slashes '/' in the URL path.
	highestRank := -1
	for _, rule := range s.RuleSet.rules {
		if rule != nil {
			if matchRule(r.URL, rule) {
				rank := rankMatch(rule)
				if rank > highestRank {
					bestMatch = rule
					highestRank = rank
				}
			}
		}
	}

	// No rules matched
	if bestMatch == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Get the handler for the best matching rule
	handler, err := getHandlerFromRule(bestMatch, s.defaultServePath)
	if err != nil {
		msg := fmt.Sprintf("error getting handler: %s", err)
		log.Error().Msg(msg)

		_, err := w.Write([]byte(msg))
		if err != nil {
			log.Error().Err(err).Msg("error writing error message to response")
		}
		w.WriteHeader(http.StatusInternalServerError)

		return
	}

	// Serve the rule handler.
	log.Info().Msgf("Serving request for URL %s to %s", r.URL, bestMatch.Matcher)
	handler.ServeHTTP(w, r)
}

func (s *ProxyServer) TailscaleListener(addr, hostname, tsStatePath string, funnelEnabled bool) (net.Listener, error) {
	var listener net.Listener
	tsnet := &tsnet.Server{
		Hostname: hostname,
		Dir:      tsStatePath,
		Logf: func(msg string, args ...any) {
			s.logger.Debug().Msgf(msg, args...)
		},
	}

	if funnelEnabled {
		ln, err := tsnet.ListenFunnel("tcp", addr)
		if err != nil {
			return nil, err
		}
		listener = ln
	} else {
		ln, err := tsnet.Listen("tcp", addr)
		if err != nil {
			return nil, err
		}
		listener = ln
	}
	return listener, nil
}

func (s *ProxyServer) Listener(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

// Determine whether a rule matches a URL
func matchRule(requestURL *url.URL, rule *node.ReverseProxyRule) bool {
	// TODO make this work with actual glob strings
	// For now, just match based off of base path
	if strings.HasPrefix(requestURL.Path, rule.Matcher) {
		prefixRemoved := strings.TrimPrefix(requestURL.Path, rule.Matcher)
		if prefixRemoved == "" {
			return true
		}
		return strings.HasPrefix(prefixRemoved, "/")
	}
	return false

}

// Find the rank of a match, given a requestURL and a rule
func rankMatch(rule *node.ReverseProxyRule) int {
	return strings.Count(rule.Matcher, "/")
}
