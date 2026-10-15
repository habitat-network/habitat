package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestLoadShippedConfig(t *testing.T) {
	scenarios, err := loadScenarios("scenarios.json")
	require.NoError(t, err)
	require.NotEmpty(t, scenarios)

	setups, err := loadSetups("setups.json")
	require.NoError(t, err)
	require.NotEmpty(t, setups)
	for _, s := range setups {
		require.True(t, filepath.IsAbs(s.MCPConfig), "mcp config resolved relative to setups file")
		_, err := os.Stat(s.MCPConfig)
		require.NoError(t, err, "setup %s mcp config exists", s.Name)
	}
}

func TestLoadScenariosValidates(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"unknown expectation", `[{"id":"a","prompt":"p","expect":"maybe"}]`},
		{"missing prompt", `[{"id":"a","expect":"search"}]`},
		{
			"duplicate id",
			`[{"id":"a","prompt":"p","expect":"search"},{"id":"a","prompt":"q","expect":"none"}]`,
		},
		{"invalid json", `[`},
		{
			"prompt and turns",
			`[{"id":"a","prompt":"p","expect":"search","turns":[{"prompt":"q","expect":"none"}]}]`,
		},
		{
			"turn missing prompt",
			`[{"id":"a","turns":[{"prompt":"q","expect":"none"},{"expect":"none"}]}]`,
		},
		{"turn unknown expectation", `[{"id":"a","turns":[{"prompt":"q","expect":"maybe"}]}]`},
		{"neither prompt nor turns", `[{"id":"a","expect":"search"}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFile(t, t.TempDir(), "scenarios.json", tt.content)
			_, err := loadScenarios(path)
			require.Error(t, err)
		})
	}
}

func TestLoadSetupsValidates(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cfg.json", `{"mcpServers":{}}`)
	tests := []struct {
		name    string
		content string
	}{
		{"missing name", `[{"mcp_config":"cfg.json"}]`},
		{"missing config file", `[{"name":"a","mcp_config":"nope.json"}]`},
		{
			"duplicate name",
			`[{"name":"a","mcp_config":"cfg.json"},{"name":"a","mcp_config":"cfg.json"}]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFile(t, dir, "setups.json", tt.content)
			_, err := loadSetups(path)
			require.Error(t, err)
		})
	}
}

func TestLoadScenariosNormalizesToTurns(t *testing.T) {
	path := writeFile(t, t.TempDir(), "scenarios.json", `[
		{"id":"one","prompt":"p","expect":"search","answer_contains":["x"]},
		{"id":"two","turns":[
			{"prompt":"q1","expect":"search"},
			{"prompt":"q2","expect":"none","answer_contains":["y"]}]}]`)
	got, err := loadScenarios(path)
	require.NoError(t, err)
	require.Equal(t, []Turn{{Prompt: "p", Expect: ExpectSearch, AnswerContains: []string{"x"}}},
		got[0].Turns, "a single-turn scenario is one turn")
	require.Equal(t, []Turn{
		{Prompt: "q1", Expect: ExpectSearch},
		{Prompt: "q2", Expect: ExpectNone, AnswerContains: []string{"y"}},
	}, got[1].Turns)
}
