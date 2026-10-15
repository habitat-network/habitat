package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeClaude writes a shell script standing in for the claude CLI: it records
// its arguments and working directory, then prints transcript as stream-json.
func fakeClaude(t *testing.T, transcript string, exitCode int) (bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args.txt")
	fixture := writeFile(t, dir, "fixture.jsonl", transcript)
	script := "#!/bin/sh\n" +
		"pwd > '" + argsFile + "'\n" +
		"for a in \"$@\"; do echo \"$a\" >> '" + argsFile + "'; done\n" +
		"cat '" + fixture + "'\n" +
		"echo 'some stderr' >&2\n" +
		"exit " + string(rune('0'+exitCode)) + "\n"
	bin = writeFile(t, dir, "claude", script)
	require.NoError(t, os.Chmod(bin, 0o700))
	return bin, argsFile
}

func TestRunnerRunsClaudeAndParsesTranscript(t *testing.T) {
	bin, argsFile := fakeClaude(t, sampleTranscript, 0)
	r := claudeRunner{
		bin: bin, model: "sonnet", maxTurns: 7, maxBudgetUSD: 0.5, timeout: time.Minute,
	}
	setup := Setup{
		Name:            "direct",
		MCPConfig:       "/abs/cfg.json",
		Strict:          true,
		DisallowedTools: []string{"mcp__claude_ai_Claude_Docs"},
	}
	transcriptPath := filepath.Join(t.TempDir(), "out", "run.jsonl")

	got, err := r.run(context.Background(), setup, "What meetings?", transcriptPath)
	require.NoError(t, err)
	require.Len(t, got.Calls, 2)
	require.NotNil(t, got.Result)

	saved, err := os.ReadFile(transcriptPath)
	require.NoError(t, err)
	require.Equal(t, sampleTranscript, string(saved))

	recorded, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(recorded)), "\n")
	cwd, args := lines[0], strings.Join(lines[1:], " ")
	testDir, err := os.Getwd()
	require.NoError(t, err)
	require.NotEqual(t, testDir, cwd, "claude runs in an isolated directory")
	for _, want := range []string{
		"-p What meetings?",
		"--output-format stream-json --verbose",
		"--mcp-config /abs/cfg.json",
		"--strict-mcp-config",
		"--setting-sources project",
		"--no-session-persistence",
		"--max-turns 7",
		"--max-budget-usd 0.5",
		"--model sonnet",
		"mcp__claude_ai_Claude_Docs",
		"mcp__habitat__Granola_sign_out",
	} {
		require.Contains(t, args, want)
	}
}

func TestRunnerReportsNonZeroExitWithStderr(t *testing.T) {
	bin, _ := fakeClaude(t, "", 1)
	r := claudeRunner{bin: bin, maxTurns: 1, timeout: time.Minute}
	_, err := r.run(
		context.Background(), Setup{Name: "a", MCPConfig: "/cfg.json"}, "hi",
		filepath.Join(t.TempDir(), "run.jsonl"),
	)
	require.ErrorContains(t, err, "some stderr")
}

func TestPreflightReportsMissingTools(t *testing.T) {
	bin, _ := fakeClaude(t, sampleTranscript, 0)
	r := claudeRunner{bin: bin, maxTurns: 1, timeout: time.Minute}
	setup := Setup{
		Name:                "direct",
		MCPConfig:           "/cfg.json",
		RequireToolPrefixes: []string{"mcp__habitat__", "mcp__claude_ai_Google_Drive__"},
	}
	err := r.preflight(context.Background(), setup, t.TempDir())
	require.ErrorContains(t, err, "mcp__claude_ai_Google_Drive__")
	require.NotContains(t, err.Error(), "mcp__habitat__,")

	setup.RequireToolPrefixes = []string{"mcp__habitat__", "mcp__granola__"}
	require.NoError(t, r.preflight(context.Background(), setup, t.TempDir()))
}
