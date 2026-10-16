package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// defaultAllowedTools may run without a permission prompt in every setup.
// Anything else that needs permission is denied (headless runs can't prompt)
// but still shows up in the transcript as an attempted call.
var defaultAllowedTools = []string{
	"mcp__" + habitatServer,
	"Read", "Grep", "Glob", "WebSearch", "WebFetch",
}

// defaultDisallowedTools are hidden from the model in every setup: anything
// that writes locally, and Habitat's proxied Granola sign-out, which would
// disconnect the account under test.
var defaultDisallowedTools = []string{
	"Bash", "Write", "Edit", "NotebookEdit",
	"mcp__" + habitatServer + "__Granola_sign_out",
}

// claudeRunner runs prompts through the claude CLI in headless mode.
type claudeRunner struct {
	bin string
	// model is passed as --model when set; otherwise the CLI's default.
	model        string
	maxTurns     int
	maxBudgetUSD float64
	timeout      time.Duration
	// sessionsDir is where claude saves sessions, one directory per working
	// directory (~/.claude/projects). A conversation's session is deleted from
	// it afterwards; empty leaves it.
	sessionsDir string
}

// session is how a turn joins a conversation. The zero value is a one-off run
// whose session isn't saved.
type session struct {
	id string
	// resume continues the saved session id instead of starting it.
	resume bool
}

func (r claudeRunner) args(setup Setup, prompt string, s session) []string {
	args := []string{
		"-p",
		prompt,
		"--output-format",
		"stream-json",
		"--verbose",
		"--mcp-config",
		setup.MCPConfig,
		// Only project settings, from an empty directory: keeps the user's
		// plugins, hooks and permission rules out of the eval while still
		// using their stored MCP OAuth tokens.
		"--setting-sources",
		"project",
		"--max-turns",
		strconv.Itoa(r.maxTurns),
		"--allowedTools",
		strings.Join(slices.Concat(defaultAllowedTools, setup.AllowedTools), ","),
		"--disallowedTools",
		strings.Join(slices.Concat(defaultDisallowedTools, setup.DisallowedTools), ","),
	}
	switch {
	case s.id == "":
		args = append(args, "--no-session-persistence")
	case s.resume:
		args = append(args, "--resume", s.id)
	default:
		args = append(args, "--session-id", s.id)
	}
	if setup.Strict {
		args = append(args, "--strict-mcp-config")
	}
	if r.maxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(r.maxBudgetUSD, 'f', -1, 64))
	}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}
	return args
}

// run runs prompt under setup, saving the raw stream-json output to
// transcriptPath, and parses it. claude runs in a fresh empty directory so no
// CLAUDE.md or project .mcp.json leaks in.
func (r claudeRunner) run(
	ctx context.Context, setup Setup, prompt string, transcriptPath string,
) (Transcript, error) {
	workDir, err := os.MkdirTemp("", "mcp-evals-run-*")
	if err != nil {
		return Transcript{}, err
	}
	defer func() { _ = os.RemoveAll(workDir) }()
	return r.runTurn(ctx, setup, workDir, prompt, session{}, transcriptPath)
}

// turnRun is one turn of a conversation: its transcript, or why it has none.
type turnRun struct {
	transcript Transcript
	err        error
}

// runConversation runs prompts as the turns of one claude session, each turn
// a separate claude process resuming the last, in the same fresh directory.
// transcriptPath names each 1-based turn's transcript. Once a turn fails to
// run, the rest aren't run.
func (r claudeRunner) runConversation(
	ctx context.Context, setup Setup, prompts []string, transcriptPath func(turn int) string,
) []turnRun {
	runs := make([]turnRun, len(prompts))
	workDir, err := os.MkdirTemp("", "mcp-evals-run-*")
	if err != nil {
		for i := range runs {
			runs[i].err = err
		}
		return runs
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	s := session{id: newSessionID()}
	defer r.deleteSession(s.id)
	failed := 0
	for i, prompt := range prompts {
		if failed > 0 {
			runs[i].err = fmt.Errorf("not run: turn %d failed", failed)
			continue
		}
		runs[i].transcript, runs[i].err = r.runTurn(
			ctx, setup, workDir, prompt, s, transcriptPath(i+1),
		)
		if runs[i].err != nil {
			failed = i + 1
		}
		s.resume = true
	}
	return runs
}

// deleteSession removes the session claude saved for id, so eval
// conversations don't pile up in the user's resumable sessions. It's best
// effort: a session left behind is harmless.
func (r claudeRunner) deleteSession(id string) {
	if r.sessionsDir == "" {
		return
	}
	paths, _ := filepath.Glob(filepath.Join(r.sessionsDir, "*", id+".jsonl"))
	dirs, _ := filepath.Glob(filepath.Join(r.sessionsDir, "*", id))
	for _, p := range slices.Concat(paths, dirs) {
		_ = os.RemoveAll(p)
		// Removes the per-directory project folder too, once it's empty.
		_ = os.Remove(filepath.Dir(p))
	}
}

// newSessionID returns a random (version 4) UUID, the form claude's
// --session-id takes.
func newSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// runTurn runs prompt as one claude process in workDir, joining session s.
func (r claudeRunner) runTurn(
	ctx context.Context, setup Setup, workDir, prompt string, s session, transcriptPath string,
) (Transcript, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.bin, r.args(setup, prompt, s)...)
	cmd.Dir = workDir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	// Save the transcript even for a failed run, to debug it.
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0o750); err != nil {
		return Transcript{}, err
	}
	if err := os.WriteFile(transcriptPath, stdout.Bytes(), 0o600); err != nil {
		return Transcript{}, err
	}
	if runErr != nil {
		return Transcript{}, fmt.Errorf(
			"claude: %w: %s", runErr, strings.TrimSpace(stderr.String()),
		)
	}
	return parseTranscript(&stdout)
}

// preflight checks setup's MCP servers are connected and offering tools,
// with a trivial prompt, so a server needing (re)authentication fails fast
// instead of quietly counting as "the model didn't pick it".
func (r claudeRunner) preflight(ctx context.Context, setup Setup, outDir string) error {
	cheap := r
	cheap.model, cheap.maxTurns = "haiku", 1
	prompt := "Reply with just OK."
	if setup.RequireSearchHits != "" {
		// Room to load the deferred tool, call it and answer.
		cheap.maxTurns = 4
		prompt = fmt.Sprintf(
			"Call Habitat's search_records tool once with the query %q, then reply with "+
				"just the number of hits.", setup.RequireSearchHits,
		)
	}
	t, err := cheap.run(
		ctx, setup, prompt,
		filepath.Join(outDir, "preflight", string(setup.Name)+".jsonl"),
	)
	if err != nil {
		return err
	}
	var missing []string
	for _, prefix := range setup.RequireToolPrefixes {
		if !slices.ContainsFunc(t.Tools, func(tool string) bool {
			return strings.HasPrefix(tool, prefix)
		}) {
			missing = append(missing, prefix)
		}
	}
	if len(missing) > 0 {
		statuses := make([]string, len(t.Servers))
		for i, s := range t.Servers {
			statuses[i] = s.Name + "=" + s.Status
		}
		return fmt.Errorf(
			"no tools offered matching %s (servers: %s); authenticate them first, see README",
			strings.Join(missing, ", "), strings.Join(statuses, ", "),
		)
	}
	if setup.RequireSearchHits != "" {
		return checkSearchHits(t, setup.RequireSearchHits)
	}
	return nil
}

// checkSearchHits checks t called search_records and it found something for
// query.
func checkSearchHits(t Transcript, query string) error {
	called := false
	for _, call := range t.Calls {
		if call.Name != searchTool {
			continue
		}
		called = true
		var result struct {
			Hits []json.RawMessage `json:"hits"`
		}
		if json.Unmarshal([]byte(call.Result), &result) == nil && len(result.Hits) > 0 {
			return nil
		}
	}
	if !called {
		return fmt.Errorf("preflight didn't call %s to check for seeded records", searchTool)
	}
	return fmt.Errorf(
		"%s found nothing for %q: seed the org with fixtures/org (see README)", searchTool, query,
	)
}
