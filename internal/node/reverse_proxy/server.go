package reverse_proxy

import (
	"net"
	"net/http"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/config"
	"github.com/eagraf/habitat-new/internal/node/constants"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/rs/zerolog"

	"tailscale.com/tsnet"
)

func NewProcessProxyRuleStateUpdateSubscriber(ruleSet RuleSet) (*hdb.IdempotentStateUpdateSubscriber, error) {
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
	Rules      RuleSet
}

func NewProxyServer(logger *zerolog.Logger, config *config.NodeConfig) *ProxyServer {
	return &ProxyServer{
		logger:     logger,
		Rules:      make(RuleSet),
		nodeConfig: config,
	}
}

func (s *ProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for _, rule := range s.Rules {
		if rule.Match(r.URL) {
			rule.Handler().ServeHTTP(w, r)
			return
		}
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
			Hostname: constants.DefaultTSNetHostname,
			Dir:      s.nodeConfig.TailScaleStatePath(),
			Logf: func(msg string, args ...any) {
				s.logger.Debug().Msgf(msg, args...)
			},
		}

		ln, err := tsnet.Listen("tcp", addr)
		if err != nil {
			return nil, err
		}
		listener = ln
	}

	return listener, nil
}
