package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

// fakeClaudeSessions is like fakeClaude, but appends each invocation's working
// directory and arguments to argsFile, separated by "---", and saves a session
// file under sessionsDir for --session-id, as the real CLI does.
func fakeClaudeSessions(
	t *testing.T, transcript, sessionsDir string,
) (bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args.txt")
	fixture := writeFile(t, dir, "fixture.jsonl", transcript)
	script := "#!/bin/sh\n" +
		"echo --- >> '" + argsFile + "'\n" +
		"pwd >> '" + argsFile + "'\n" +
		"prev=\n" +
		"for a in \"$@\"; do\n" +
		"  echo \"$a\" >> '" + argsFile + "'\n" +
		"  if [ \"$prev\" = --session-id ]; then\n" +
		"    mkdir -p '" + sessionsDir + "/proj' && touch '" + sessionsDir + "/proj/'\"$a\".jsonl\n" +
		"  fi\n" +
		"  prev=$a\n" +
		"done\n" +
		"cat '" + fixture + "'\n"
	bin = writeFile(t, dir, "claude", script)
	require.NoError(t, os.Chmod(bin, 0o700))
	return bin, argsFile
}

// invocations splits fakeClaudeSessions' argsFile into each call's working
// directory and arguments.
func invocations(t *testing.T, argsFile string) (dirs []string, args [][]string) {
	t.Helper()
	recorded, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	for _, call := range strings.Split(string(recorded), "---\n")[1:] {
		lines := strings.Split(strings.TrimSuffix(call, "\n"), "\n")
		dirs = append(dirs, lines[0])
		args = append(args, lines[1:])
	}
	return dirs, args
}

func TestRunnerRunsConversationInOneSession(t *testing.T) {
	sessionsDir := t.TempDir()
	bin, argsFile := fakeClaudeSessions(t, sampleTranscript, sessionsDir)
	r := claudeRunner{bin: bin, maxTurns: 3, timeout: time.Minute, sessionsDir: sessionsDir}
	out := t.TempDir()
	path := func(turn int) string { return filepath.Join(out, fmt.Sprintf("t%d.jsonl", turn)) }

	got := r.runConversation(
		context.Background(), Setup{Name: "a", MCPConfig: "/cfg.json"},
		[]string{"first", "second", "third"}, path,
	)
	require.Len(t, got, 3)
	for i, turn := range got {
		require.NoError(t, turn.err)
		require.Len(t, turn.transcript.Calls, 2)
		require.FileExists(t, path(i+1), "each turn's transcript is saved")
	}

	dirs, args := invocations(t, argsFile)
	require.Len(t, args, 3)
	require.Equal(t, dirs[0], dirs[1], "every turn runs in the same directory")
	require.Equal(t, dirs[0], dirs[2])
	argValue := func(args []string, flag string) string {
		i := slices.Index(args, flag)
		require.NotEqual(t, -1, i, "%s in %v", flag, args)
		return args[i+1]
	}
	id := argValue(args[0], "--session-id")
	require.NotEmpty(t, id)
	require.Equal(t, "first", argValue(args[0], "-p"))
	require.NotContains(t, args[0], "--no-session-persistence")
	for i, prompt := range []string{"second", "third"} {
		require.Equal(t, id, argValue(args[i+1], "--resume"), "later turns resume the session")
		require.Equal(t, prompt, argValue(args[i+1], "-p"))
		require.NotContains(t, args[i+1], "--session-id")
	}

	sessions, err := filepath.Glob(filepath.Join(sessionsDir, "*", "*.jsonl"))
	require.NoError(t, err)
	require.Empty(t, sessions, "the saved session is deleted afterwards")
}

func TestRunnerStopsConversationAfterFailedTurn(t *testing.T) {
	bin, _ := fakeClaude(t, "", 1)
	r := claudeRunner{bin: bin, maxTurns: 1, timeout: time.Minute, sessionsDir: t.TempDir()}
	out := t.TempDir()
	got := r.runConversation(
		context.Background(), Setup{Name: "a", MCPConfig: "/cfg.json"},
		[]string{"first", "second"},
		func(turn int) string { return filepath.Join(out, fmt.Sprintf("t%d.jsonl", turn)) },
	)
	require.Len(t, got, 2)
	require.ErrorContains(t, got[0].err, "some stderr")
	require.ErrorContains(t, got[1].err, "turn 1", "later turns aren't run")
}

func TestPreflightChecksSeededSearch(t *testing.T) {
	transcript := func(result string) string {
		return `{"type":"system","subtype":"init","tools":["mcp__habitat__search_records"],` +
			`"mcp_servers":[{"name":"habitat","status":"connected"}]}` + "\n" +
			`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"a",` +
			`"name":"mcp__habitat__search_records","input":{"query":"Kestrel"}}]}}` + "\n" +
			`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"a",` +
			`"content":[{"type":"text","text":` + result + `}]}]}}` + "\n" +
			`{"type":"result","is_error":false,"result":"1"}` + "\n"
	}
	setup := Setup{
		Name: "a", MCPConfig: "/cfg.json", RequireSearchHits: "Kestrel",
		RequireToolPrefixes: []string{"mcp__habitat__search_records"},
	}
	preflight := func(transcript string) (args string, err error) {
		bin, argsFile := fakeClaude(t, transcript, 0)
		r := claudeRunner{bin: bin, maxTurns: 1, timeout: time.Minute}
		err = r.preflight(context.Background(), setup, t.TempDir())
		recorded, readErr := os.ReadFile(argsFile)
		require.NoError(t, readErr)
		return string(recorded), err
	}

	args, err := preflight(transcript(`"{\"hits\":[{\"uri\":\"x\"}]}"`))
	require.NoError(t, err)
	require.Contains(t, args, "Kestrel", "the prompt asks for the seeded query")

	_, err = preflight(transcript(`"{\"hits\":[]}"`))
	require.ErrorContains(t, err, "Kestrel")

	_, err = preflight(sampleTranscript)
	require.ErrorContains(t, err, "didn't call", "not calling search is an error too")
}
