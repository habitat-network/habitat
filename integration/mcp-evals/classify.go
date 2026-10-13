package main

import (
	"encoding/json"
	"strings"
)

// Source is where a tool call fetched data from: SourceHabitat,
// SourceHabitatGateway, SourceMeta, "mcp:<server>" for another MCP server, or
// "builtin:<tool>" for a built-in Claude Code tool.
type Source string

const (
	// SourceHabitat is one of Habitat's own tools (see habitatOwnTools).
	SourceHabitat Source = "habitat"
	// SourceHabitatGateway is a connected server's tool proxied through
	// Habitat's MCP gateway.
	SourceHabitatGateway Source = "habitat-gateway"
	// SourceMeta is a tool that doesn't fetch data itself, like ToolSearch.
	SourceMeta Source = "meta"
)

func (s Source) isHabitat() bool {
	return s == SourceHabitat || s == SourceHabitatGateway
}

// habitatServer is the name every setup's MCP config must give the Habitat
// server, so its tools are named mcp__habitat__<tool>.
const habitatServer = "habitat"

// habitatOwnTools are the tools internal/mcpserver defines itself; every
// other mcp__habitat__ tool is proxied from a connected server.
var habitatOwnTools = map[string]bool{
	"get_record":     true,
	"get_blob":       true,
	"search_records": true,
}

// metaTools are built-in tools that don't fetch data themselves. Calls a
// subagent (Agent) makes are recorded on their own, so Agent is meta too.
var metaTools = map[string]bool{
	"ToolSearch":      true,
	"TodoWrite":       true,
	"Skill":           true,
	"Agent":           true,
	"Task":            true,
	"AskUserQuestion": true,
	"EnterPlanMode":   true,
	"ExitPlanMode":    true,
}

// mcpResourceTools are built-in tools that read another MCP server's
// resources, naming the server in their "server" input.
var mcpResourceTools = map[string]bool{
	"ListMcpResourcesTool": true,
	"ReadMcpResourceTool":  true,
}

func classify(call ToolCall) Source {
	if rest, ok := strings.CutPrefix(call.Name, "mcp__"); ok {
		server, tool, _ := strings.Cut(rest, "__")
		return mcpSource(server, tool)
	}
	if mcpResourceTools[call.Name] {
		var input struct {
			Server string `json:"server"`
		}
		// A missing or malformed input lists every server's resources.
		if json.Unmarshal(call.Input, &input) != nil || input.Server == "" {
			return SourceMeta
		}
		if input.Server == habitatServer {
			return SourceHabitat
		}
		return Source("mcp:" + input.Server)
	}
	if metaTools[call.Name] {
		return SourceMeta
	}
	return Source("builtin:" + call.Name)
}

func mcpSource(server, tool string) Source {
	switch {
	case server != habitatServer:
		return Source("mcp:" + server)
	case habitatOwnTools[tool]:
		return SourceHabitat
	default:
		return SourceHabitatGateway
	}
}
