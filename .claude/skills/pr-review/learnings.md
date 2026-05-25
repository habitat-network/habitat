## 2026-05-20 | AT Protocol Lexicon Defs Constraints
- confidence: high
- observation: The AT Protocol spec prohibits ref-type types from being top-level named definitions in `defs` — refs can only be used inline within other schemas.
- action: When sharing an object definition between multiple properties (e.g. `subject` and `resource`), define the shared type as an `object` def and have the parent schema ref it directly, rather than creating ref-only defs.

## 2026-05-20 | AuthZEN Action.Properties Type
- confidence: high
- observation: The AuthZEN spec (Section 5.3) defines action `properties` as an object, not a string — it supports key-value pairs like `{"method": "GET"}`.
- action: Keep `properties` as `{"type": "object"}` in authzen lexicons, and reference the spec when asked about changing it.

## 2026-05-20 | Lexgen Output Directory
- confidence: high
- observation: `cmd/lexgen` has `runFromWorkspaceRoot: true` in moon config, meaning the `outdir` in `lexgen.json` is relative to the workspace root, not `cmd/lexgen/`.
- action: When running lexgen manually, use the pre-built `bin/lexgen` binary from the workspace root, or use `moon :generate` to ensure correct working directory.
