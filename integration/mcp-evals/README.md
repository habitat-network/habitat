# mcp-evals

Measures whether agents call Habitat's search tool
(`mcp__habitat__search_records`, see `internal/mcpserver`) whenever a prompt
needs company context, and leave Habitat alone when it doesn't. Each
scenario's prompt runs through headless Claude Code (`claude -p`) and is
scored on which tools it called.

Not part of `moon ci`: runs use model quota, results vary between runs, and
they depend on your real connected accounts. Run it by hand before and after
changing tool descriptions or server instructions.

Runs use `haiku` by default to save quota; pass `-model sonnet` (or `-model ""`
for the CLI's default) to check a result holds on a stronger model. Compare
results only across runs on the same model. The report's "est. API cost" is
Claude Code's API-price estimate; when you're logged in with a claude.ai
subscription, runs draw on its usage limits instead of being billed.

## Setup

`setups.json` lists the MCP configs to run under; for now just
`habitat-only` (`configs/habitat-only.json`, an org's Habitat MCP endpoint
with `--strict-mcp-config`). The config must name the server `habitat`.

Headless runs reuse the OAuth tokens Claude Code has stored for each server
(keyed by server name and a hash of its config, including `headers`), but can't complete an OAuth flow themselves.
Before the first run (and again if the URL changes), from this directory run
`claude --mcp-config configs/habitat-only.json`, open `/mcp`, and authenticate
`habitat`.

Each setup runs a cheap preflight first and stops if it doesn't offer
`search_records` (e.g. Habitat needs auth or search isn't configured), or, with
`require_search_hits`, if searching for it finds nothing.

### Seeding the org

Scenarios assume the org holds the documents in `fixtures/org/`, about a
fictional company, Kestrel. Each holds a fact found nowhere else (the table is
in `docs/superpowers/plans/2026-10-15-mcp-multi-turn-eval.md`), which
`answer_contains` checks for. Upload every file through Drop in the org the
setup points at; `habitat-only`'s preflight checks a search for "Kestrel"
finds them. If uploads don't show up in search, check your membership has the
`member` role: Drop shares files with members, and an org's first admin may
only have `admin`.

## Running

```bash
cd integration/mcp-evals
go run .                         # every scenario × 5 runs
go run . -runs 1                 # quick pass
go run . -scenario roadmap-cuts -model sonnet
```

### Comparing wordings

`-variants variants.json` runs every setup once per variant, as setups named
`<setup>@<variant>`, so one summary compares them. A variant replaces the
server instructions and/or `search_records` description Habitat sends; a field
left out keeps pear's built-in text, and `""` sends none. The harness sends
them in the `X-Habitat-Mcp-Text-Overrides` header, which pear only honours
with `--mcp_text_overrides` (`pear:dev` sets it; restart it after pulling
this). Without it every variant silently measures the built-in text. The
header comes from a generated `headersHelper` script rather than the config's
`headers`, because Claude Code keys stored OAuth tokens on a hash of the
config including `headers`: a header there would need its own login per
variant, and a server it reports as needing auth stays skipped for a while
(`~/.claude/mcp-needs-auth-cache.json`), even under the original config.

```bash
go run . -variants variants.json -runs 2
go run . -variants variants.json -variant builtin,neither
```

Results go to `tmp/<timestamp>/` (gitignored, and the transcripts contain your
real data):

- `summary.md`: pass rates, and the first tool each scenario's runs called
  (also printed to stdout)
- `results.jsonl`: one scored run per line, including the tool sequence and
  any `ToolSearch` queries (the words to put in tool descriptions)
- `transcripts/<setup>/<scenario>-<run>.jsonl`: raw stream-json for each run,
  or `<scenario>-<run>-t<turn>.jsonl` for each turn of a multi-turn one

## Scenarios and scoring

Each scenario in `scenarios.json` is an `id`, a `prompt` and an `expect`:

- `search`: the prompt needs company context (contracts, roadmap, policies,
  past conversations, ...), so it passes if `search_records` is called at all.
- `none`: the prompt needs no company context, so it passes if no Habitat tool
  is called.

An optional `answer_contains` also requires the answer to contain one of its
strings (case-insensitively), e.g. a planted fact.

A multi-turn scenario has `turns` instead: a list of `{prompt, expect,
answer_contains}`, sent in order in one session (each turn a `claude -p`
process resuming the last, in the same directory). Each turn is scored on
its own calls, so a later `search` turn passes only if it searched again. The
summary's "Later turns" section gives pass rates for turns 2+, where earlier
context could stand in for searching, and how many conversations passed every
turn. The eval deletes each conversation's saved session afterwards.

Turns that time out, exceed the budget or end in an error count as errors and
are left out of pass rates; after a turn fails to run, its conversation stops.

## Isolation

Each run happens in a fresh empty directory with `--setting-sources project`,
so your plugins, hooks, project `.mcp.json` and repo CLAUDE.md stay out of the
run. Local-write tools (`Bash`, `Write`, `Edit`) and Habitat's proxied Granola
`sign_out` tool are disallowed. Your user-level `~/.claude/CLAUDE.md` may
still load.
