package reverse_proxy

import (
	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/rs/zerolog/log"
)

type ReverseProxyRestorer struct {
	ruleSet *RuleSet
}

func (r *ReverseProxyRestorer) Restore(restoreEvent hdb.StateUpdate) error {
	nodeState := restoreEvent.NewState().(*node.State)
	if nodeState.ReverseProxyRules == nil {
		return nil
	}

	for _, rule := range *nodeState.ReverseProxyRules {
		log.Info().Msgf("Restoring rule %s, matcher: %s", rule.ID, rule.Matcher)
		err := r.ruleSet.AddRule(rule)
		if err != nil {
			log.Error().Msgf("error restoring rule: %s", err)
		}
	}

	return nil
}
