package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ScenarioID names a scenario.
type ScenarioID string

// Expectation is how a scenario's run is scored.
type Expectation string

const (
	// ExpectSearch passes when Habitat's search tool (searchTool) is called:
	// the prompt needs company context.
	ExpectSearch Expectation = "search"
	// ExpectNone passes when no Habitat tool is called: the prompt needs no
	// company context.
	ExpectNone Expectation = "none"
)

// Scenario is one conversation to run: either a single prompt (Prompt,
// Expect, AnswerContains) or several Turns. loadScenarios normalizes both to
// Turns, which is what the rest of the harness reads.
type Scenario struct {
	ID             ScenarioID  `json:"id"`
	Prompt         string      `json:"prompt,omitempty"`
	Expect         Expectation `json:"expect,omitempty"`
	AnswerContains []string    `json:"answer_contains,omitempty"`
	// Turns are the user messages of a multi-turn conversation, sent in order
	// in one session.
	Turns []Turn `json:"turns,omitempty"`
}

// Turn is one user message in a scenario's conversation, scored on what the
// model did in response to it alone.
type Turn struct {
	Prompt string      `json:"prompt"`
	Expect Expectation `json:"expect"`
	// AnswerContains, if set, also requires the turn's answer to contain one
	// of these strings (case-insensitively), e.g. a fact only a Habitat record
	// holds.
	AnswerContains []string `json:"answer_contains,omitempty"`
}

// SetupName names a setup.
type SetupName string

// Setup is one set of MCP servers to offer the model.
type Setup struct {
	Name SetupName `json:"name"`
	// MCPConfig is a `claude --mcp-config` file, relative to the setups file
	// when loaded (and absolute afterwards). It must name the Habitat server
	// habitatServer.
	MCPConfig string `json:"mcp_config"`
	// Strict passes --strict-mcp-config, which also drops claude.ai
	// connectors such as Google Drive.
	Strict bool `json:"strict"`
	// RequireToolPrefixes must each prefix at least one offered tool, checked
	// by claudeRunner.preflight, so a server needing auth fails the setup
	// rather than silently skewing results.
	RequireToolPrefixes []string `json:"require_tool_prefixes"`
	// RequireSearchHits, if set, is a query search_records must find
	// something for, checked by claudeRunner.preflight, so an org that hasn't
	// been seeded (see README) fails the setup.
	RequireSearchHits string `json:"require_search_hits,omitempty"`
	// DisallowedTools are hidden from the model, on top of the runner's
	// defaults.
	DisallowedTools []string `json:"disallowed_tools,omitempty"`
	// AllowedTools are allowed without a permission prompt, on top of the
	// runner's defaults.
	AllowedTools []string `json:"allowed_tools,omitempty"`
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	return nil
}

func loadScenarios(path string) ([]Scenario, error) {
	var scenarios []Scenario
	if err := readJSON(path, &scenarios); err != nil {
		return nil, err
	}
	seen := map[ScenarioID]bool{}
	for i := range scenarios {
		s := &scenarios[i]
		var errs []error
		if s.ID == "" {
			errs = append(errs, errors.New("missing id"))
		}
		if seen[s.ID] {
			errs = append(errs, errors.New("duplicate id"))
		}
		seen[s.ID] = true
		single := s.Prompt != "" || s.Expect != "" || len(s.AnswerContains) > 0
		switch {
		case single && len(s.Turns) > 0:
			errs = append(errs, errors.New("has both prompt and turns"))
		case single:
			s.Turns = []Turn{{Prompt: s.Prompt, Expect: s.Expect, AnswerContains: s.AnswerContains}}
		case len(s.Turns) == 0:
			errs = append(errs, errors.New("missing prompt"))
		}
		for n, turn := range s.Turns {
			if turn.Prompt == "" {
				errs = append(errs, fmt.Errorf("turn %d: missing prompt", n+1))
			}
			switch turn.Expect {
			case ExpectSearch, ExpectNone:
			default:
				errs = append(errs, fmt.Errorf("turn %d: unknown expect %q", n+1, turn.Expect))
			}
		}
		if err := errors.Join(errs...); err != nil {
			return nil, fmt.Errorf("scenario %q: %w", s.ID, err)
		}
	}
	return scenarios, nil
}

func loadSetups(path string) ([]Setup, error) {
	var setups []Setup
	if err := readJSON(path, &setups); err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	seen := map[SetupName]bool{}
	for i := range setups {
		s := &setups[i]
		if s.Name == "" {
			return nil, fmt.Errorf("setup %d: missing name", i)
		}
		if seen[s.Name] {
			return nil, fmt.Errorf("setup %q: duplicate name", s.Name)
		}
		seen[s.Name] = true
		if !filepath.IsAbs(s.MCPConfig) {
			s.MCPConfig = filepath.Join(dir, s.MCPConfig)
		}
		if _, err := os.Stat(s.MCPConfig); err != nil {
			return nil, fmt.Errorf("setup %q: %w", s.Name, err)
		}
	}
	return setups, nil
}
