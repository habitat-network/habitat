# mcp-evals

Measures whether agents reach for Habitat's MCP server (`internal/mcpserver`)
first when they need data. Each scenario's prompt runs through headless Claude
Code (`claude -p`) under each setup, and the run is scored on where its first
data-fetching tool call went.

Not part of `moon ci`: runs cost money, results vary between runs, and they
depend on your real connected accounts. Run it by hand before and after
changing tool descriptions or server instructions.

## Setups (`setups.json`)

- `habitat-only`: just Habitat (`--strict-mcp-config`).
- `habitat-vs-direct`: Habitat plus a direct Granola server and your claude.ai
  Google Drive connector, i.e. the same data offered by competing tools.

Every setup's MCP config must name the Habitat server `habitat`.

## One-time auth

Headless runs reuse the OAuth tokens Claude Code has stored for each server
(keyed by server name and URL), but can't complete an OAuth flow themselves.
Before the first run:

1. Habitat and direct Granola: from this directory, run
   `claude --mcp-config configs/habitat-vs-direct.json`, open `/mcp`, and
   authenticate `habitat` and `granola`.
2. Google Drive: connect or reconnect the Google Drive connector in your
   claude.ai settings.

Each setup runs a cheap preflight first and stops with the failing servers'
status if any of its `require_tool_prefixes` has no tools.

## Running

```bash
cd integration/mcp-evals
go run .                                    # all setups × scenarios × 5 runs
go run . -setup habitat-only -runs 1        # quick pass
go run . -scenario gateway-recent-meeting -model haiku
```

Results go to `tmp/<timestamp>/` (gitignored, and the transcripts contain your
real data):

- `summary.md`: pass rates by setup and category, plus where each scenario's
  runs went first (also printed to stdout)
- `results.jsonl`: one scored run per line, including the tool sequence and
  any `ToolSearch` queries (the words to put in tool descriptions)
- `transcripts/<setup>/<scenario>-<run>.jsonl`: raw stream-json for each run

## Scoring

Tool calls are classified as `habitat` (Habitat's own tools), `habitat-gateway`
(connected servers' tools proxied through Habitat), `mcp:<server>`,
`builtin:<tool>`, or `meta` (e.g. `ToolSearch`, `Agent`; skipped when finding the first
data call). A scenario's `expect` decides pass/fail:

- `habitat`: the first data call goes to Habitat (either kind).
- `none`: Habitat is never called (negative controls).
- `any`: reported but not scored.

Runs that time out, exceed the budget or end in an error count as errors and
are left out of pass rates.

## Isolation

Each run happens in a fresh empty directory with `--setting-sources project`,
so your plugins, hooks, project `.mcp.json` and repo CLAUDE.md stay out of the
run. Local-write tools (`Bash`, `Write`, `Edit`) and the Granola `sign_out`
tools are disallowed. Your user-level `~/.claude/CLAUDE.md` may still load.
