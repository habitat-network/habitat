package reverse_proxy

import (
	"encoding/json"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/rs/zerolog/log"
)

type ProcessProxyRulesExecutor struct {
	RuleSet RuleSet
}

func (e *ProcessProxyRulesExecutor) TransitionType() string {
	return node.TransitionStartProcess
}

func (e *ProcessProxyRulesExecutor) ShouldExecute(update hdb.StateUpdate) (bool, error) {
	// This process is very lightweight, so we can just execute it every time
	return true, nil
}

func (e *ProcessProxyRulesExecutor) Execute(update hdb.StateUpdate) error {
	var processStartTransition node.ProcessStartTransition
	err := json.Unmarshal(update.Transition(), &processStartTransition)
	if err != nil {
		return err
	}

	nodeState := update.NewState().(*node.State)
	for _, rule := range *nodeState.ReverseProxyRules {
		if rule.AppID == processStartTransition.AppID {
			log.Info().Msgf("Adding reverse proxy rule %v", rule)
			err = e.RuleSet.AddRule(rule)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// TODO remove rule when process is stopped

func (e *ProcessProxyRulesExecutor) PostHook(update hdb.StateUpdate) error {
	return nil
}
