package main

import (
	"bytes"
	"context"
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
}

func (r claudeRunner) args(setup Setup, prompt string) []string {
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
		"--no-session-persistence",
		"--max-turns",
		strconv.Itoa(r.maxTurns),
		"--allowedTools",
		strings.Join(slices.Concat(defaultAllowedTools, setup.AllowedTools), ","),
		"--disallowedTools",
		strings.Join(slices.Concat(defaultDisallowedTools, setup.DisallowedTools), ","),
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
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	workDir, err := os.MkdirTemp("", "mcp-evals-run-*")
	if err != nil {
		return Transcript{}, err
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.bin, r.args(setup, prompt)...)
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
	t, err := cheap.run(
		ctx, setup, "Reply with just OK.",
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
	return nil
}
