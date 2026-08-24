// Worker entry point, pointed to by wrangler.jsonc's `main`.
//
// `@tanstack/react-start/server-entry` is itself a valid Workers module
// default-exporting a `fetch` handler, and could be used as `main` directly
// — but a Workers module's Durable Object bindings must name classes
// exported by that same entry module, and a package export can't be
// extended with our own exports. So this file re-exports Start's handler
// as its own default export and adds every DO class beside it.
export { default } from "@tanstack/react-start/server-entry";

export { PingRoom } from "./rooms/ping";
