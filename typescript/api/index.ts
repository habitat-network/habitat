/**
 * Hand-written barrel over `@atproto/lex` `lex build` output.
 *
 * Re-exports the generated top-level namespaces so consumers can reach
 * method schemas and types, e.g. `network.habitat.groups.listGroups.main` or
 * `network.habitat.groups.defs.GroupView`.
 */
export * as com from "./src/generated/com.js"
export * as group from "./src/generated/group.js"
export * as network from "./src/generated/network.js"
export * from "./src/opensocial.js"
