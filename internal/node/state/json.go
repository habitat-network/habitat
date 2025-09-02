package state

import (
	"fmt"

	jsonpatch "github.com/evanphx/json-patch/v5"
)

// This file contains helpers for JSON patches.

// TODO: find a way to make this private, since it really shouldn't be exported
// Right now it's only used in internal/node/controller/processes_test.go for a fileDB mock
// We can do this by makeing fileDB take in a persist func() and just having that be empty in tests or moving the mock into this package

// Helper function to statelessly generate new state from old + transitions
// Returns the new state, wrappers for logging / debugging purposes, error if any
func ProposeTransitions(old SerializedState, transitions []Transition) (SerializedState, []*TransitionWrapper, error) {
	wrappers := make([]*TransitionWrapper, 0)
	// Copy old state into temp var to apply transitions to
	tmp := old

	for _, t := range transitions {
		err := t.Validate(tmp)
		if err != nil {
			return nil, nil, fmt.Errorf("transition validation failed: %s", err)
		}

		patch, err := t.Patch(tmp)
		if err != nil {
			return nil, nil, err
		}

		tmp, err = applyPatch(tmp, patch)
		if err != nil {
			return nil, nil, err
		}

		wrapped, err := WrapTransition(t, patch, tmp)
		if err != nil {
			return nil, nil, err
		}

		wrappers = append(wrappers, wrapped)
	}

	return tmp, wrappers, nil
}

// Helper functions to do actual JSON patching
func applyPatch(state []byte, patchJSON []byte) ([]byte, error) {
	updated, err := applyImpl(state, patchJSON)
	if err != nil {
		return nil, err
	}

	return updated, err
}

func applyImpl(state []byte, patchJSON []byte) ([]byte, error) {
	patch, err := jsonpatch.DecodePatch(patchJSON)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON patch: %s", err)
	}

	updated, err := patch.Apply(state)
	if err != nil {
		return nil, fmt.Errorf("error applying patch to current state: %s", err)
	}

	// check that updated state still fulfills the schema
	err = Schema.ValidateState(updated)
	if err != nil {
		return nil, fmt.Errorf("error validating updated state: %s", err)
	}
	return updated, nil
}
