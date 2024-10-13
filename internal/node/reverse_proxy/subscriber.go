package reverse_proxy

import (
	"encoding/json"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/rs/zerolog/log"
)

func NewProcessProxyRuleSubscriber(ruleSet *RuleSet) (*hdb.IdempotentStateUpdateSubscriber, error) {
	return hdb.NewIdempotentStateUpdateSubscriber(
		"ProcessProxyRulesSubscriber",
		node.SchemaName,
		[]hdb.IdempotentStateUpdateExecutor{
			&ProcessProxyRulesExecutor{
				RuleSet: ruleSet,
			},
			&AddProxyRulesExecutor{
				RuleSet: ruleSet,
			},
		},
		&ReverseProxyRestorer{
			ruleSet: ruleSet,
		},
	)
}

// ProcesProxyRulesExecutor enables relevant reverse proxy rules when a process is started
type ProcessProxyRulesExecutor struct {
	RuleSet *RuleSet
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

type AddProxyRulesExecutor struct {
	RuleSet *RuleSet
}

func (e *AddProxyRulesExecutor) TransitionType() string {
	return node.TransitionAddReverseProxyRule
}

func (e *AddProxyRulesExecutor) ShouldExecute(update hdb.StateUpdate) (bool, error) {
	// This process is lightweight, so we can execute it every time
	return true, nil
}

func (e *AddProxyRulesExecutor) Execute(update hdb.StateUpdate) error {
	var addRuleTransition node.AddReverseProxyRuleTransition
	err := json.Unmarshal(update.Transition(), &addRuleTransition)
	if err != nil {
		return err
	}

	log.Info().Msgf("Adding new reverse proxy rule: %v", addRuleTransition.Rule)
	err = e.RuleSet.AddRule(addRuleTransition.Rule)
	if err != nil {
		return err
	}

	return nil
}

func (e *AddProxyRulesExecutor) PostHook(update hdb.StateUpdate) error {
	return nil
}
