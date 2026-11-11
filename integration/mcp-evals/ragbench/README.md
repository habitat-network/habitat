# ragbench

Measures the quality of what pear's MCP server returns: does `get_context`
find the documents that answer a question, and does it suggest the tool that
would? It answers with numbers, on data whose right answers are known, without
real accounts, a running pear, or (for most of it) a model.

Run by hand, locally. Nothing here runs in `moon ci`.

## How it works

[EnterpriseRAG-Bench](https://github.com/onyx-dot-app/EnterpriseRAG-Bench) is
a synthetic company ("Redwood Inference") with 512,000 documents across Slack,
Google Drive, GitHub and six more apps, and 500 questions that each name the
documents answering them.

1. `corpus` takes a **slice** of it: the questions answered from the apps
   being mocked, their answering documents, some documents from the same
   channel, repository or folder as each (the hard distractors), and random
   ones from each app. The same `-seed` gives the same slice.
2. `mockmcp` serves the slice through **mock MCP servers** for Google Drive,
   Slack and GitHub. Each offers its real server's search tool, with the real
   name, arguments and answer shape, a tool to read one document, and the
   names and descriptions of the real server's other tools (which do nothing,
   but are what tool suggestion has to choose among). Search is BM25 over the
   app's documents. Every call is logged.
3. `pearmcp` runs **pear's real MCP server** (`internal/mcpserver`, with
   `internal/mcpsearch` behind `get_context`) in the same process, with the
   mocks as the org's connected servers. They're listed at the real servers'
   URLs, so pear's own search providers match them unchanged; only the
   address dialed is the mock's.
4. `ragbench retrieval` asks `get_context` every question and writes what came
   back to `samples.jsonl`, with a report in `summary.md`.
5. `scorer/` (Python) scores `samples.jsonl` with
   [Ragas](https://docs.ragas.io/en/stable/).

## Setup

Tools come from proto (`.prototools` pins Go, Python and uv); `proto install`
gets them.

Fetch the dataset once (about 2 GB, a few minutes), outside the repository:

```bash
moon run integration:ragbench-dataset     # into ~/.cache/habitat/enterprise-rag-bench
```

The dataset asks to be kept out of training corpora, and this repository is
public: never commit its documents, or results holding them (`tmp/` is
ignored).

## Running

```bash
moon run integration:ragbench                         # every question, default slice
moon run integration:ragbench -- -match all           # strict keyword matching
moon run integration:ragbench -- -types semantic -questions 20
```

or, from `integration/mcp-evals`,
`go run ./ragbench retrieval -dataset <dir> [flags]` (`-h` lists the flags;
`$RAGBENCH_DATASET` sets the dataset). A run over all 139 questions takes
about 20 seconds, most of it reading documents. Results go to
`tmp/ragbench-<time>/`.

Then score the samples with Ragas:

```bash
moon run ragbench-scorer:score -- <absolute path to samples.jsonl>
moon run ragbench-scorer:score -- <samples.jsonl> --llm     # model-judged metrics too
```

`--llm` needs `ANTHROPIC_API_KEY` (or `--provider openai` and
`OPENAI_API_KEY`); `--model` picks the judge.

### Experiments

Each flag is a thing to vary between two runs, comparing their `summary.md`:

| Flag | What it changes |
|---|---|
| `-match any\|all` | How the mock apps' search matches. `any` ranks documents with any of the query's words: a forgiving engine. `all` only matches documents with every word, which is closer to the apps' own keyword search and shows what passing a whole sentence as the query costs. |
| `-selector lexical\|decision` | How pear ranks tools to suggest. `decision` needs `-decisions-url` (and `$HABITAT_DECISIONS_API_KEY`). |
| `-max-tools` | How many tools `get_context` is asked to suggest. |
| `-apps` | Which apps are connected. |
| `-types`, `-questions` | Which questions run. |
| `-siblings`, `-distractors`, `-seed` | How large and how hard the slice is. |

To measure a change to pear itself (a provider's arguments, result limits, the
selector), run before and after it with the same flags.

### Pointing an agent at it

```bash
go run ./ragbench serve -dataset <dir>
```

keeps the servers up and prints an MCP config for pear's endpoint, to try
`get_context` and `call_tool` by hand or run an agent against (`claude
--mcp-config <file> --strict-mcp-config`). `get_record` and `get_blob` have
nothing behind them here.

## Reading the results

`summary.md`, overall and by question type and app:

- **get_context recall**: the share of a question's answering documents among
  the results. **hit rate** is the share of questions with at least one;
  **MRR** is 1 over the position of the first, averaged (results come grouped
  by app in the order servers are listed, so a low MRR with a high recall
  means the answer was there but far down).
- **direct recall**: the same, asking each app's search for the question
  directly, 5 results from each. That's what `get_context` would return if
  pear lost nothing on the way, so a gap between the two is pear's doing, and
  "Lost on the way" lists those questions.
- **one index recall**: the best 10 results of a single index over every
  app's documents: what pear could return if it held the documents itself
  rather than asking each app.
- **Tool suggestion**: a question's reference tool is the search tool of the
  app holding its answer. hit@k is the share of questions with it among the
  first k suggestions.

`samples.jsonl` has one line per question: the question, its reference answer
and documents, what `get_context` returned (ids and text), both yardsticks,
the suggested tools, and every call the mocks received. The scorer adds
`ragas-scores.jsonl` and `ragas-summary.md` next to it:

- `id_context_recall`, `id_context_precision`: Ragas's id-based retrieval
  metrics. Recall matches `summary.md`'s.
- `tool_f1_top3`: Ragas's ToolCallF1, treating the first 3 suggestions as the
  calls an agent following them would make (`--top-tools` changes the 3).
- with `--llm`, `context_recall`: whether the text `get_context` returned is
  enough to support the reference answer, judged by a model. Samples carrying
  an agent's `response` also get `factual_correctness` and `faithfulness`.

## What the numbers do and don't mean

- The mocks' ranking is BM25, not Slack's, Drive's or GitHub's. Absolute
  recall says how findable the answers are in this corpus by keyword, not how
  pear will do against the real apps. Compare runs with each other, and
  `get_context` with the yardsticks.
- Documents and questions were both written by a model, so questions share
  more words with their answers than real ones do. `-match all` and the
  `semantic` question type push the other way.
- Only Slack, Drive and GitHub are mocked, the apps pear has search providers
  for: 139 of the benchmark's 500 questions. The rest need Jira, Confluence,
  Linear, Gmail, HubSpot or Fireflies, or have no answering document.
- The mocks' other tools carry the real servers' names with descriptions
  written here, not copied from them. Check a tool-suggestion result against
  the real servers before relying on it.
- Ragas's judged metrics use its own rubrics, so they aren't comparable with
  the benchmark's leaderboard.

## Adding to it

- Another app: a file in `mockmcp` like `github.go` and a case in
  `mockmcp.New`, once pear has a provider for it in
  `internal/mcpsearch/providers.go`.
- Another metric: Go ones in `metrics.go` and `report.go`; Ragas ones in
  `scorer/src/ragbench_scorer/score.py`, which reads any field a sample has.
