package reverse_proxy

import (
	"net/http"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/config"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/rs/zerolog"
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
