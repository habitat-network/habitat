// Command mcp-evals measures whether agents reach for Habitat's MCP server
// first when fetching data. It runs each scenario's prompt through headless
// Claude Code under each setup (a set of MCP servers, e.g. Habitat alone, or
// Habitat alongside direct connections to the same upstream services),
// records which tools the model called, and scores whether it invoked Habitat
// (and how strongly it preferred Habitat over duplicate tools). See README.md.
package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mcp-evals:", err)
		os.Exit(1)
	}
}

type job struct {
	setup    Setup
	scenario Scenario
	run      int
}

func run() error {
	setupsPath := flag.String("setups", "setups.json", "setups file")
	scenariosPath := flag.String("scenarios", "scenarios.json", "scenarios file")
	variantsPath := flag.String(
		"variants", "",
		"variants file (e.g. variants.json): run every setup once per wording of Habitat's "+
			"instructions and search description; needs pear with --mcp_text_overrides",
	)
	onlyVariants := flag.String("variant", "", "comma-separated variant ids to run (default all)")
	onlySetups := flag.String("setup", "", "comma-separated setup names to run (default all)")
	onlyScenarios := flag.String(
		"scenario",
		"",
		"comma-separated scenario ids to run (default all)",
	)
	runs := flag.Int("runs", 5, "runs per scenario per setup")
	concurrency := flag.Int("concurrency", 3, "runs in flight at once")
	model := flag.String(
		"model", "haiku",
		"model passed to claude --model; the cheapest by default, empty for the CLI's default",
	)
	maxTurns := flag.Int("max-turns", 15, "claude --max-turns per run")
	maxBudget := flag.Float64("max-budget-usd", 1, "claude --max-budget-usd per run (0 for none)")
	timeout := flag.Duration("timeout", 5*time.Minute, "timeout per run")
	outDir := flag.String("out", "", "output directory (default tmp/<timestamp>)")
	claudeBin := flag.String("claude", "claude", "claude CLI binary")
	flag.Parse()

	setups, err := loadSetups(*setupsPath)
	if err != nil {
		return err
	}
	setups = filterBy(setups, *onlySetups, func(s Setup) string { return string(s.Name) })
	scenarios, err := loadScenarios(*scenariosPath)
	if err != nil {
		return err
	}
	scenarios = filterBy(scenarios, *onlyScenarios, func(s Scenario) string { return string(s.ID) })
	if len(setups) == 0 || len(scenarios) == 0 {
		return fmt.Errorf("nothing to run: %d setups, %d scenarios", len(setups), len(scenarios))
	}
	if *outDir == "" {
		*outDir = filepath.Join("tmp", time.Now().Format("20060102-150405"))
	}
	if *variantsPath != "" {
		variants, err := loadVariants(*variantsPath)
		if err != nil {
			return err
		}
		variants = filterBy(variants, *onlyVariants, func(v Variant) string { return string(v.ID) })
		if len(variants) == 0 {
			return errors.New("nothing to run: 0 variants")
		}
		if setups, err = applyVariants(
			setups, variants, filepath.Join(*outDir, "mcp-configs"),
		); err != nil {
			return err
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	sessionsDir, err := claudeSessionsDir()
	if err != nil {
		return err
	}
	runner := claudeRunner{
		bin: *claudeBin, model: *model, maxTurns: *maxTurns, maxBudgetUSD: *maxBudget,
		timeout: *timeout, sessionsDir: sessionsDir,
	}

	for _, s := range setups {
		fmt.Fprintf(os.Stderr, "preflight %s...\n", s.Name)
		if err := runner.preflight(ctx, s, *outDir); err != nil {
			return fmt.Errorf("preflight %s: %w", s.Name, err)
		}
	}

	var jobs []job
	for _, s := range setups {
		for _, sc := range scenarios {
			for i := 1; i <= *runs; i++ {
				jobs = append(jobs, job{setup: s, scenario: sc, run: i})
			}
		}
	}
	results := runJobs(ctx, runner, jobs, *concurrency, *outDir)
	slices.SortFunc(results, func(a, b RunResult) int {
		return cmp.Or(
			cmp.Compare(a.Setup, b.Setup), cmp.Compare(a.Scenario, b.Scenario),
			cmp.Compare(a.Run, b.Run), cmp.Compare(a.Turn, b.Turn),
		)
	})

	if err := writeResults(*outDir, results); err != nil {
		return err
	}
	md := renderMarkdown(results)
	if err := os.WriteFile(filepath.Join(*outDir, "summary.md"), []byte(md), 0o600); err != nil {
		return err
	}
	fmt.Println(md)
	fmt.Fprintf(os.Stderr, "results in %s\n", *outDir)
	return ctx.Err()
}

// runJobs runs jobs with up to concurrency in flight, saving each turn's
// transcript under outDir/transcripts, and scores every turn.
func runJobs(
	ctx context.Context, runner claudeRunner, jobs []job, concurrency int, outDir string,
) []RunResult {
	var (
		mu      sync.Mutex
		results []RunResult
		done    int
		wg      sync.WaitGroup
	)
	queue := make(chan job)
	for range max(concurrency, 1) {
		wg.Go(func() {
			for j := range queue {
				turns := j.scenario.Turns
				runs := runJob(ctx, runner, j, outDir)
				mu.Lock()
				done++
				for i, tr := range runs {
					var r RunResult
					if tr.err != nil {
						r = scoreTurnError(
							j.setup.Name,
							j.scenario.ID,
							j.run,
							i+1,
							turns[i],
							tr.err,
						)
					} else {
						r = scoreTurn(
							j.setup.Name,
							j.scenario.ID,
							j.run,
							i+1,
							turns[i],
							tr.transcript,
						)
					}
					results = append(results, r)
					turn := ""
					if len(turns) > 1 {
						turn = fmt.Sprintf(" t%d", r.Turn)
					}
					fmt.Fprintf(os.Stderr, "[%d/%d] %s %s #%d%s: %s (first: %s)\n",
						done, len(jobs), r.Setup, r.Scenario, r.Run, turn, r.Outcome,
						cmp.Or(r.FirstTool, "none"))
				}
				mu.Unlock()
			}
		})
	}
	for _, j := range jobs {
		if ctx.Err() != nil {
			break
		}
		queue <- j
	}
	close(queue)
	wg.Wait()
	return results
}

// runJob runs j's scenario once: a single turn on its own, or every turn in
// one session.
func runJob(ctx context.Context, runner claudeRunner, j job, outDir string) []turnRun {
	dir := filepath.Join(outDir, "transcripts", string(j.setup.Name))
	turns := j.scenario.Turns
	if len(turns) == 1 {
		path := filepath.Join(dir, fmt.Sprintf("%s-%d.jsonl", j.scenario.ID, j.run))
		t, err := runner.run(ctx, j.setup, turns[0].Prompt, path)
		return []turnRun{{transcript: t, err: err}}
	}
	prompts := make([]string, len(turns))
	for i, turn := range turns {
		prompts[i] = turn.Prompt
	}
	return runner.runConversation(ctx, j.setup, prompts, func(turn int) string {
		return filepath.Join(dir, fmt.Sprintf("%s-%d-t%d.jsonl", j.scenario.ID, j.run, turn))
	})
}

// claudeSessionsDir is where claude saves sessions: projects/ under
// $CLAUDE_CONFIG_DIR, or ~/.claude by default.
func claudeSessionsDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "projects"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

func writeResults(outDir string, results []RunResult) error {
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return err
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	for _, r := range results {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(outDir, "results.jsonl"), []byte(b.String()), 0o600)
}

// filterBy keeps the items whose name is in the comma-separated list, or all
// of them if the list is empty.
func filterBy[T any](items []T, list string, name func(T) string) []T {
	if list == "" {
		return items
	}
	want := strings.Split(list, ",")
	return slices.DeleteFunc(slices.Clone(items), func(item T) bool {
		return !slices.Contains(want, name(item))
	})
}
