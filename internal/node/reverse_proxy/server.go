package reverse_proxy

import (
	"net"
	"net/http"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/config"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/rs/zerolog"

	"tailscale.com/tsnet"
)

func NewProcessProxyRuleStateUpdateSubscriber(ruleSet *RuleSet) (*hdb.IdempotentStateUpdateSubscriber, error) {
	return hdb.NewIdempotentStateUpdateSubscriber(
		"ProcessProxyRulesSubscriber",
		node.SchemaName,
		[]hdb.IdempotentStateUpdateExecutor{
			&ProcessProxyRulesExecutor{
				RuleSet: ruleSet,
			},
		},
		&ReverseProxyRestorer{
			ruleSet: ruleSet,
		},
	)
}

type ProxyServer struct {
	logger     *zerolog.Logger
	nodeConfig *config.NodeConfig
	RuleSet    *RuleSet
}

func NewProxyServer(logger *zerolog.Logger, config *config.NodeConfig) *ProxyServer {
	return &ProxyServer{
		logger: logger,
		RuleSet: &RuleSet{
			rules:        make(map[string]RuleHandler),
			baseFilePath: config.WebBundlePath(),
		},
		nodeConfig: config,
	}
}

func (s *ProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var bestMatch RuleHandler = nil
	// Find the matching rule with the highest "rank", aka the most slashes '/' in the URL path.
	highestRank := -1
	for _, rule := range s.RuleSet.rules {
		if rule.Match(r.URL) {
			if rule.Rank() > highestRank {
				bestMatch = rule
				highestRank = rule.Rank()
			}
		}
	}

	// Serve the handler with the best matching rule.
	if bestMatch != nil {
		bestMatch.Handler().ServeHTTP(w, r)
		return
	}
	// No rules matched
	w.WriteHeader(http.StatusNotFound)
}

func (s *ProxyServer) Listener(addr string) (net.Listener, error) {
	// If TS_AUTHKEY is set, create a tsnet listener. Otherwise, create a normal tcp listener.
	var listener net.Listener
	if s.nodeConfig.TailscaleAuthkey() == "" {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return nil, err
		}
		listener = ln
	} else {
		tsnet := &tsnet.Server{
			Hostname: s.nodeConfig.Hostname(),
			Dir:      s.nodeConfig.TailScaleStatePath(),
			Logf: func(msg string, args ...any) {
				s.logger.Debug().Msgf(msg, args...)
			},
		}

		if s.nodeConfig.TailScaleFunnelEnabled() {
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
	}

	return listener, nil
}
