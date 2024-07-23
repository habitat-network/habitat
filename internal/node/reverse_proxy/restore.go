package reverse_proxy

import (
	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/rs/zerolog/log"
)

type ReverseProxyRestorer struct {
	ruleSet RuleSet
}

func (r *ReverseProxyRestorer) Restore(restoreEvent hdb.StateUpdate) error {
	nodeState := restoreEvent.NewState().(*node.State)
	for _, process := range nodeState.Processes {
		rules, err := nodeState.GetReverseProxyRulesForProcess(process.ID)
		if err != nil {
			return err
		}

		for _, rule := range rules {
			log.Info().Msgf("Restoring rule %s", rule)
			err = r.ruleSet.AddRule(rule)
			if err != nil {
				log.Error().Msgf("error restoring rule: %s", err)
			}
		}
	}
	return nil
}
