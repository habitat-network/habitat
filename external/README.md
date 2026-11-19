# external

Git submodules of external repositories that are relevant to Habitat, kept here as reference material (not built or imported by Habitat code).

| Path | Source | Why |
| --- | --- | --- |
| `opensocial-proposal/` | https://tangled.org/opensocial.group/proposal | The opensocial group proposal, partially implemented in `internal/opensocial`. |
| `bluesky-proposals/` | https://github.com/bluesky-social/proposals | AT Protocol proposals, including 0016 permissioned data (spaces), implemented in `internal/spaces`. |
| `effect/` | https://github.com/Effect-TS/effect | Effect source code, used as a reference for idiomatic Effect patterns. |
| `jetstream/` | https://github.com/bluesky-social/jetstream | Full-network archive, replay, and streaming service for atproto, relevant to the firehose syncing done by `sap`. |

Initialize with `git submodule update --init --recursive`.
