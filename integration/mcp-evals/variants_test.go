package main

import (
	"encoding/base64"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadShippedVariants(t *testing.T) {
	variants, err := loadVariants("variants.json")
	require.NoError(t, err)
	require.NotEmpty(t, variants)
}

func TestLoadVariantsValidates(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"missing id", `[{"instructions":"x"}]`},
		{"duplicate id", `[{"id":"a"},{"id":"a"}]`},
		{"invalid json", `[`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFile(t, t.TempDir(), "variants.json", tt.content)
			_, err := loadVariants(path)
			require.Error(t, err)
		})
	}
}

func TestApplyVariants(t *testing.T) {
	dir := t.TempDir()
	cfg := writeFile(t, dir, "cfg.json", `{"mcpServers":{
		"habitat":{"type":"http","url":"https://habitat.example/mcp/org"},
		"other":{"type":"http","url":"https://other.example"}}}`)
	text := func(s string) *string { return &s }
	setups := []Setup{{Name: "only", MCPConfig: cfg, Strict: true}}
	variants := []Variant{
		{ID: "builtin"},
		{ID: "bare", Instructions: text(""), SearchDescription: text("Search records.")},
	}

	// A relative dir, as -out gives by default: claude runs from another
	// directory, so the configs' paths must still resolve.
	t.Chdir(t.TempDir())
	got, err := applyVariants(setups, variants, "configs")
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.True(t, filepath.IsAbs(got[0].MCPConfig), got[0].MCPConfig)
	require.Equal(t, SetupName("only@builtin"), got[0].Name)
	require.Equal(t, SetupName("only@bare"), got[1].Name)
	require.True(t, got[1].Strict, "other setup fields are kept")

	type server struct {
		URL           string            `json:"url"`
		Headers       map[string]string `json:"headers"`
		HeadersHelper string            `json:"headersHelper"`
	}
	habitat := func(s Setup) server {
		var c struct {
			MCPServers map[string]server `json:"mcpServers"`
		}
		require.NoError(t, readJSON(s.MCPConfig, &c))
		require.Contains(t, c.MCPServers, "other", "other servers are kept")
		return c.MCPServers["habitat"]
	}

	builtin := habitat(got[0])
	require.Equal(t, "https://habitat.example/mcp/org", builtin.URL)
	require.Empty(t, builtin.HeadersHelper, "no overrides, no header")

	bare := habitat(got[1])
	require.Equal(t, "https://habitat.example/mcp/org", bare.URL, "url unchanged, so auth applies")
	// Claude Code keys stored OAuth tokens on the server config including
	// headers but not headersHelper, so the override must come from the helper.
	require.Empty(t, bare.Headers)
	out, err := exec.Command(bare.HeadersHelper).Output()
	require.NoError(t, err)
	var headers map[string]string
	require.NoError(t, json.Unmarshal(out, &headers))
	decoded, err := base64.StdEncoding.DecodeString(headers[textOverridesHeader])
	require.NoError(t, err)
	// The same JSON internal/mcpserver.TextOverrides parses.
	require.JSONEq(t, `{"instructions":"","search_description":"Search records."}`, string(decoded))
}

func TestApplyVariantsRejectsExistingHeadersHelper(t *testing.T) {
	dir := t.TempDir()
	cfg := writeFile(t, dir, "cfg.json",
		`{"mcpServers":{"habitat":{"type":"http","url":"x","headersHelper":"h"}}}`)
	text := func(s string) *string { return &s }
	_, err := applyVariants(
		[]Setup{{Name: "s", MCPConfig: cfg}},
		[]Variant{{ID: "v", Instructions: text("i")}}, t.TempDir(),
	)
	require.Error(t, err)
}

func TestApplyVariantsRequiresHabitatServer(t *testing.T) {
	dir := t.TempDir()
	cfg := writeFile(t, dir, "cfg.json", `{"mcpServers":{"other":{"type":"http","url":"x"}}}`)
	_, err := applyVariants(
		[]Setup{{Name: "s", MCPConfig: cfg}}, []Variant{{ID: "v"}}, t.TempDir(),
	)
	require.Error(t, err)
}
