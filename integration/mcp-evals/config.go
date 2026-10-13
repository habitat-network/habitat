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

// Category groups scenarios for reporting.
type Category string

const (
	// CategoryNative asks for data Habitat stores itself.
	CategoryNative Category = "native"
	// CategoryGateway asks for data a connected server holds.
	CategoryGateway Category = "gateway"
	// CategoryAmbiguous could be answered from several sources.
	CategoryAmbiguous Category = "ambiguous"
	// CategoryNegative shouldn't touch Habitat at all.
	CategoryNegative Category = "negative"
)

// Expectation is how a scenario's run is scored.
type Expectation string

const (
	// ExpectHabitat passes when the first data-fetching call goes to Habitat.
	ExpectHabitat Expectation = "habitat"
	// ExpectNone passes when Habitat is never called.
	ExpectNone Expectation = "none"
	// ExpectAny is reported but not scored.
	ExpectAny Expectation = "any"
)

// Scenario is one prompt to run.
type Scenario struct {
	ID       ScenarioID  `json:"id"`
	Category Category    `json:"category"`
	Prompt   string      `json:"prompt"`
	Expect   Expectation `json:"expect"`
	// Notes explains what the scenario probes; not used when running.
	Notes string `json:"notes,omitempty"`
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
	for _, s := range scenarios {
		var errs []error
		if s.ID == "" {
			errs = append(errs, errors.New("missing id"))
		}
		if seen[s.ID] {
			errs = append(errs, errors.New("duplicate id"))
		}
		seen[s.ID] = true
		if s.Prompt == "" {
			errs = append(errs, errors.New("missing prompt"))
		}
		switch s.Category {
		case CategoryNative, CategoryGateway, CategoryAmbiguous, CategoryNegative:
		default:
			errs = append(errs, fmt.Errorf("unknown category %q", s.Category))
		}
		switch s.Expect {
		case ExpectHabitat, ExpectNone, ExpectAny:
		default:
			errs = append(errs, fmt.Errorf("unknown expect %q", s.Expect))
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
