# Multi-turn MCP eval

Extend `integration/mcp-evals` so it can measure whether agents call Habitat's
`search_records` on **later turns** of a conversation, not just the first.

## Why
In a Claude Desktop session, after two turns that searched Habitat (competitive
research, Granola notes), the third question ("how well does this fit Habitat's
thesis and pitch?") was answered from earlier context instead of searching for
the uploaded pitch deck. The single-turn eval can't see this: every scenario is
one prompt against an empty org, so there's never "good enough" earlier context
to lean on.

The server instructions and `search_records` description now ask agents to
search again for each new question and to look up specific documents directly
(see `internal/mcpserver/mcpserver.go`). This eval measures whether that works,
and whether it causes over-searching on follow-ups that don't need it.

## Findings so far
- `claude -p --resume <session-id>` continues a conversation in print mode:
  turn 2 keeps the session id and remembers turn 1 (checked 2026-10-15). Each
  turn is its own process, so MCP servers reconnect per turn. Still to verify
  with pear up: Habitat connects on the resumed turn. Fallback if not: one
  process with `--input-format stream-json`, writing the next user message to
  stdin after each `result` event.
- Drop files are searchable: `internal/search/blobs.go` indexes text/plain,
  text/markdown, text/html, JSON and PDF blobs up to 5MB.

## 1. Seed data (manual for now)
`integration/mcp-evals/fixtures/org/` holds documents for a fictional company,
**Kestrel** (warehouse-ops software for 3PLs). A fictional company keeps the
planted facts clean: the model can't know them without reading the docs.

Seeding: upload every file in `fixtures/org/` through Drop in the local org
(`did:web:wxbq0h.pear.local.habitat.network`), then wait for the indexer to
pick them up. If a `.md` file uploads without a `text/markdown` type and isn't
searchable, re-upload it as `.txt`.

Seeding changes the single-turn results too (searches now find things), so
take fresh baselines after seeding; don't compare with pre-seed runs.

### Planted facts
| File | Fact (only in that file) |
|---|---|
| kestrel-pitch-deck.md | raising a $2.4M pre-seed; ICP is 3PLs running 2 to 10 warehouses; 6 paying pilots, $41k ARR |
| competitive-research.md | DockPilot raised prices about 40% in August 2026 |
| pricing-decision-2026-09.md | $450 per warehouse per month, annual; free tier dropped |
| remote-work-policy.md | core hours 10am–2pm Mountain; quarterly week in Denver; $1,200 stipend |
| roadmap-q4-2026.md | mobile scanner app cut; Spanish localization pushed to Q1 2027 |
| team-and-owners.md | Priya Nair owns billing |
| call-northgate-logistics.md | lost two days reconciling cycle counts |
| call-bluewater-3pl.md | night shift doesn't know what day shift promised |
| call-pine-ridge-fulfillment.md | 15% off the first year if they sign by October 31 |
| planning-meeting-2026-10-06.md | Sam to hire a contract engineer to pair with Marcus |

### Preflight
Add a seeded-data check per setup: one cheap haiku run that searches Habitat for
"Kestrel" and fails the setup if `search_records` returns no hits, so an
unseeded org doesn't quietly skew results.

## 2. Scenario format
A scenario has either `prompt` + `expect` (one turn, as today) or `turns`:

```json
{
  "id": "direction-then-pitch",
  "turns": [
    {"prompt": "Based on our customer conversations and competitive research, what's a good first product direction?", "expect": "search"},
    {"prompt": "How well does that fit our thesis and pitch?", "expect": "search", "answer_contains": ["2.4M"]}
  ]
}
```

- `expect` per turn, as now: `search` passes if `search_records` is called in
  that turn; `none` passes if no Habitat tool is called in that turn.
- `answer_contains` (optional): the turn also needs one of these strings in its
  answer, case-insensitively.

## 3. Conversations
| id | turn 1 | scored turn 2 | tests |
|---|---|---|---|
| roadmap-then-pitch | what was cut from the roadmap | does that change our investor story? (deck facts) | the Desktop case: look up the specific doc |
| topic-switch | what we decided about pricing | remote-work policy (`Denver`) | search again on a new topic |
| follow-up-rewrite | what was cut from the roadmap | "make that a 3-bullet summary" → `none` | no over-searching on a follow-up |
| code-then-company | write a Python function | who owns billing? (`Priya`) | code to company context |
| company-then-trivia | open risks for our launch | capital of Australia → `none` | not stuck on Habitat |
| pricing-then-pine-ridge | what we decided about pricing | is the Pine Ridge offer consistent? (`October 31`) | follow-up that needs another doc |
| risks-then-backup | open risks for our launch | who covers Marcus's WMS work? (`Sam`) | follow-up that needs another doc |
| policy-then-onboarding | remote-work policy | a new engineer's first month (`shadow`) | follow-up that needs another doc |

The later turns that should search build on turn 1, so answering from context
is tempting, but they need a document turn 1's searches don't return. The first
run had `direction-then-pitch` and `deepen` here instead; their turn 1 searches
already returned the turn 2 document (10 docs, broad queries), so they couldn't
show whether later turns search again. The setup disallows Granola's tools, so
only the seeded docs are in play.

## 4. Harness changes
- **Config:** `Scenario` gains `Turns []Turn`; a single-turn scenario loads as
  one turn. Validation: exactly one of prompt/turns, every turn has a prompt
  and a known expect.
- **Runner:** turn 1 runs as today without `--no-session-persistence`; take
  `session_id` from the `init` event. Later turns run
  `claude -p --resume <id> <prompt>` in the same work dir. One transcript per
  turn: `transcripts/<setup>/<scenario>-<run>-t<N>.jsonl`. Delete the saved
  session (`~/.claude/projects/<work dir>`) afterwards.
- **Scoring:** one `RunResult` per turn with a `Turn` field; `answer_contains`
  checked against that turn's result text. A conversation passes when every
  turn does.
- **Summary:** add a "later turns" table (turns 2+) by setup × expect, with
  pass rate and first-tool counts: the headline number for this eval.
- **Limits:** `-max-turns`, `-timeout` and `-max-budget-usd` apply per
  conversation turn.
- **Tests (test-first):** turns validation, per-turn scoring including
  `answer_contains`, and the resume flow against a fake `claude` binary that
  prints canned stream-json.

## 5. Order
1. ~~Spike `--resume`~~ (done; Habitat reconnects on resumed turns).
2. ~~Fixture docs~~ (done, seeded via Drop). Seeding surfaced that an org's
   first admin gets `admin` without `member`, so can't search Drop files; fixed
   locally with `community.opensocial.assignRoles`, still to fix in
   `opensocial.ProvisionMember`.
3. ~~Turns in config, runner and scoring, test-first~~ (done).
4. ~~Later-turns summary table, seeded-data preflight, README~~ (done). The
   runner passes `--session-id` on turn 1 rather than reading it from `init`.
5. Baseline: `-variants variants.json -variant builtin,single-turn -runs 3`.
