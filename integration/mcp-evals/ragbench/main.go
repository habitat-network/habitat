// Command ragbench measures the quality of what pear's MCP server returns,
// using EnterpriseRAG-Bench: a synthetic company's Slack, Drive and GitHub
// with questions whose answering documents are known. It serves those
// documents through mock app servers (mockmcp), puts pear's real MCP server
// in front of them (pearmcp), and asks it each question. See ../README.md.
//
//	ragbench retrieval -dataset <dir>   score get_context's results and suggestions
//	ragbench serve -dataset <dir>       keep the servers up, to point an agent at
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/corpus"
	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/mockmcp"
	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/pearmcp"
	"github.com/habitat-network/habitat/internal/decisions"
	"github.com/habitat-network/habitat/internal/mcpserver"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ragbench:", err)
		os.Exit(1)
	}
}

var errUsage = errors.New("usage: ragbench retrieval|serve -dataset <dir> [flags]; -h lists them")

func run(args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	switch args[0] {
	case "retrieval":
		return runRetrieval(ctx, args[1:])
	case "serve":
		return runServe(ctx, args[1:])
	default:
		return errUsage
	}
}

// config is what both commands take: which part of the dataset to serve, and
// how pear and the mocks behave.
type config struct {
	dataset     string
	apps        string
	types       string
	questions   int
	siblings    int
	distractors int
	seed        uint64
	match       string
	selector    string
	decisions   struct{ url, model string }
}

func (c *config) register(fs *flag.FlagSet) {
	fs.StringVar(&c.dataset, "dataset", defaultDataset(),
		"checkout of EnterpriseRAG-Bench (or $RAGBENCH_DATASET); fetch-dataset.sh fetches it "+
			"to the default")
	fs.StringVar(&c.apps, "apps", "google-drive,slack,github",
		"comma-separated apps to mock and take questions from")
	fs.StringVar(&c.types, "types", "",
		"comma-separated question types to run, like basic,semantic (default all)")
	fs.IntVar(&c.questions, "questions", 0, "most questions to run, picked at random (default all)")
	fs.IntVar(&c.siblings, "siblings", 50,
		"documents to add from the same channel, repository or folder as each answering one")
	fs.IntVar(
		&c.distractors,
		"distractors",
		5000,
		"other documents to add from each app, at random",
	)
	fs.Uint64Var(&c.seed, "seed", 1, "seed of the random picks; the same seed gives the same slice")
	fs.StringVar(&c.match, "match", string(corpus.MatchAny),
		"how the mock apps' search matches: any (documents with any query word, ranked) or "+
			"all (only documents with every query word, like the apps' own keyword search)")
	fs.StringVar(&c.selector, "selector", "lexical",
		"how pear ranks tools to suggest: lexical, or decision (needs -decisions-url)")
	fs.StringVar(&c.decisions.url, "decisions-url", os.Getenv("HABITAT_DECISIONS_URL"),
		"decisions API for -selector decision (or $HABITAT_DECISIONS_URL); its key is read "+
			"from $HABITAT_DECISIONS_API_KEY")
	fs.StringVar(&c.decisions.model, "decisions-model", os.Getenv("HABITAT_DECISIONS_MODEL"),
		"model for -selector decision (or $HABITAT_DECISIONS_MODEL)")
}

// defaultDataset is $RAGBENCH_DATASET, or where fetch-dataset.sh puts the
// dataset.
func defaultDataset() string {
	if dir := os.Getenv("RAGBENCH_DATASET"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "habitat", "enterprise-rag-bench")
}

// settings describes c for a run's report.
func (c *config) settings() []string {
	return []string{
		"apps: " + c.apps,
		"mock search match: " + c.match,
		"tool selector: " + c.selector,
		fmt.Sprintf("slice: seed %d, %d siblings, %d distractors per app",
			c.seed, c.siblings, c.distractors),
	}
}

// start loads c's slice of the dataset and serves it through the mocks and
// pear.
func (c *config) start(addr string) (*corpus.Slice, []mockmcp.App, *pearmcp.Stack, error) {
	if c.dataset == "" {
		return nil, nil, nil, errors.New("-dataset is required")
	}
	var apps []mockmcp.App
	var sources []corpus.Source
	for _, name := range splitList(c.apps) {
		app := mockmcp.App(name)
		if !slices.Contains(mockmcp.Apps, app) {
			return nil, nil, nil, fmt.Errorf("unknown app %q", name)
		}
		apps = append(apps, app)
		sources = append(sources, app.Source())
	}
	match := corpus.Match(c.match)
	if match != corpus.MatchAny && match != corpus.MatchAll {
		return nil, nil, nil, fmt.Errorf("unknown -match %q", c.match)
	}
	var selector mcpserver.ToolSelector
	switch c.selector {
	case "lexical":
		selector = mcpserver.LexicalSelector{}
	case "decision":
		if c.decisions.url == "" {
			return nil, nil, nil, errors.New("-selector decision needs -decisions-url")
		}
		selector = mcpserver.DecisionSelector{Chooser: decisions.New(
			c.decisions.url, os.Getenv("HABITAT_DECISIONS_API_KEY"), c.decisions.model,
			&http.Client{Timeout: 30 * time.Second},
		)}
	default:
		return nil, nil, nil, fmt.Errorf("unknown -selector %q", c.selector)
	}

	dataset, err := corpus.Open(c.dataset)
	if err != nil {
		return nil, nil, nil, err
	}
	var types []corpus.QuestionType
	for _, t := range splitList(c.types) {
		types = append(types, corpus.QuestionType(t))
	}
	fmt.Fprintln(os.Stderr, "ragbench: reading documents...")
	slice, err := dataset.Slice(corpus.SliceOptions{
		Sources: sources, Types: types, MaxQuestions: c.questions,
		Siblings: c.siblings, Distractors: c.distractors, Seed: c.seed,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	if len(slice.Questions) == 0 {
		return nil, nil, nil, errors.New("no questions match -apps and -types")
	}
	fmt.Fprintln(os.Stderr, "ragbench:", slice)
	stack, err := pearmcp.Start(slice, pearmcp.Options{
		Apps: apps, Mock: mockmcp.Options{Match: match}, Selector: selector, Addr: addr,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return slice, apps, stack, nil
}

func splitList(s string) []string {
	var out []string
	for item := range strings.SplitSeq(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func runRetrieval(ctx context.Context, args []string) error {
	var c config
	fs := flag.NewFlagSet("retrieval", flag.ContinueOnError)
	c.register(fs)
	maxTools := fs.Int("max-tools", 5, "tools get_context is asked to suggest, 0 to 20")
	outDir := fs.String("out", "", "directory for results (default tmp/ragbench-<time>)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	slice, apps, stack, err := c.start("")
	if err != nil {
		return err
	}
	defer stack.Close()
	session, err := stack.Connect(ctx)
	if err != nil {
		return fmt.Errorf("connect to pear: %w", err)
	}
	defer func() { _ = session.Close() }()

	y := newYardsticks(slice, apps, corpus.Match(c.match))
	// One question at a time, so the mock servers' calls can be told apart.
	samples := make([]Sample, 0, len(slice.Questions))
	for _, q := range slice.Questions {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		samples = append(samples, runQuestion(ctx, session, stack, slice, y, q, *maxTools))
	}

	if *outDir == "" {
		*outDir = filepath.Join("tmp", "ragbench-"+time.Now().Format("20060102-150405"))
	}
	report := summary(append(c.settings(), fmt.Sprintf("max tools: %d", *maxTools)), slice, samples)
	if err := writeResults(*outDir, report, samples); err != nil {
		return err
	}
	fmt.Print(report)
	samplesPath, err := filepath.Abs(filepath.Join(*outDir, "samples.jsonl"))
	if err != nil {
		return err
	}
	fmt.Printf("\nResults in %s. Score them with Ragas:\n  moon run ragbench-scorer:score -- %s\n",
		*outDir, samplesPath)
	return nil
}

func writeResults(dir, report string, samples []Sample) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "summary.md"), []byte(report), 0o644); err != nil {
		return err
	}
	var lines strings.Builder
	for _, s := range samples {
		raw, err := json.Marshal(s)
		if err != nil {
			return err
		}
		lines.Write(raw)
		lines.WriteByte('\n')
	}
	return os.WriteFile(filepath.Join(dir, "samples.jsonl"), []byte(lines.String()), 0o644)
}

func runServe(ctx context.Context, args []string) error {
	var c config
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	c.register(fs)
	addr := fs.String("addr", "127.0.0.1:7801", "address pear's MCP server listens on")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, _, stack, err := c.start(*addr)
	if err != nil {
		return err
	}
	defer stack.Close()
	mcpConfig, err := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{
		"habitat": map[string]any{
			"type": "http", "url": stack.Endpoint,
			"headers": map[string]string{"Authorization": "Bearer " + pearmcp.Token},
		},
	}}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("pear's MCP server is at %s\nMCP config for an agent "+
		"(claude --mcp-config <file> --strict-mcp-config):\n%s\n", stack.Endpoint, mcpConfig)
	for app, url := range stack.MockEndpoints {
		fmt.Printf("mock %s: %s\n", app, url)
	}
	fmt.Println("Ctrl-C to stop.")
	<-ctx.Done()
	return nil
}
