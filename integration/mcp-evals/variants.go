package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// textOverridesHeader is internal/mcpserver.TextOverridesHeader: when pear
// runs with --mcp_text_overrides (pear:dev does), it replaces the text an MCP
// session is sent with the base64-encoded JSON of a Variant's overrides.
const textOverridesHeader = "X-Habitat-Mcp-Text-Overrides"

// VariantID names a variant.
type VariantID string

// Variant is one wording of the text Habitat's MCP server sends, run as its
// own copy of every setup to compare wordings. A nil field keeps pear's
// built-in text; an empty one sends none.
type Variant struct {
	ID VariantID `json:"id,omitempty"`
	// Instructions replaces the server instructions sent on initialize.
	Instructions *string `json:"instructions,omitempty"`
	// SearchDescription replaces the search_records tool's description.
	SearchDescription *string `json:"search_description,omitempty"`
}

func loadVariants(path string) ([]Variant, error) {
	var variants []Variant
	if err := readJSON(path, &variants); err != nil {
		return nil, err
	}
	seen := map[VariantID]bool{}
	for i, v := range variants {
		if v.ID == "" {
			return nil, fmt.Errorf("variant %d: missing id", i)
		}
		if seen[v.ID] {
			return nil, fmt.Errorf("variant %q: duplicate id", v.ID)
		}
		seen[v.ID] = true
	}
	return variants, nil
}

// applyVariants returns a copy of every setup for every variant, named
// <setup>@<variant>, whose MCP config (written under dir) sends the variant's
// overrides to the Habitat server. The OAuth tokens Claude Code stored for the
// original config still apply (see writeVariantConfig).
func applyVariants(setups []Setup, variants []Variant, dir string) ([]Setup, error) {
	// claude runs from a fresh directory, so the configs' paths must be absolute.
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	var out []Setup
	for _, s := range setups {
		for _, v := range variants {
			vs := s
			vs.Name = SetupName(fmt.Sprintf("%s@%s", s.Name, v.ID))
			vs.MCPConfig = filepath.Join(dir, string(vs.Name)+".json")
			if err := writeVariantConfig(s.MCPConfig, vs.MCPConfig, v); err != nil {
				return nil, fmt.Errorf("setup %s: %w", vs.Name, err)
			}
			out = append(out, vs)
		}
	}
	return out, nil
}

// mcpConfig is a `claude --mcp-config` file. Servers are kept as raw fields
// so fields this harness doesn't know about survive the copy.
type mcpConfig struct {
	MCPServers map[string]map[string]json.RawMessage `json:"mcpServers"`
}

// writeVariantConfig copies the MCP config at src to dst, sending v's
// overrides to the Habitat server unless v overrides nothing.
//
// Claude Code stores a server's OAuth tokens under a hash of its config,
// including any headers, so a header written into the config would need its
// own login per variant. A headersHelper isn't hashed: the header comes from a
// script written next to dst instead, which prints it.
func writeVariantConfig(src, dst string, v Variant) error {
	var cfg mcpConfig
	if err := readJSON(src, &cfg); err != nil {
		return err
	}
	server, ok := cfg.MCPServers[habitatServer]
	if !ok {
		return errors.New("mcp config has no " + habitatServer + " server")
	}
	if v.Instructions != nil || v.SearchDescription != nil {
		if _, ok := server["headersHelper"]; ok {
			return errors.New(habitatServer + " server already has a headersHelper")
		}
		overrides, err := json.Marshal(Variant{
			Instructions: v.Instructions, SearchDescription: v.SearchDescription,
		})
		if err != nil {
			return err
		}
		headers, err := json.Marshal(map[string]string{
			textOverridesHeader: base64.StdEncoding.EncodeToString(overrides),
		})
		if err != nil {
			return err
		}
		base := strings.TrimSuffix(dst, filepath.Ext(dst))
		headersPath, helperPath := base+".headers.json", base+".headers.sh"
		if err := os.WriteFile(headersPath, headers, 0o600); err != nil {
			return err
		}
		script := fmt.Sprintf("#!/bin/sh\nexec cat %q\n", headersPath)
		// The helper must be executable.
		if err := os.WriteFile(helperPath, []byte(script), 0o700); err != nil {
			return err
		}
		if server["headersHelper"], err = json.Marshal(helperPath); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}
