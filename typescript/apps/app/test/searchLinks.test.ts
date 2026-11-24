import { expect, it } from "vitest";
import { resultLink } from "../src/lib/searchLinks";
import type { SearchResultView } from "../src/server/fns/mcp";

const base: SearchResultView = {
  backend: "records",
  kind: "record",
  id: "x",
  title: null,
  url: null,
  snippet: null,
  space: null,
  collection: null,
};

it("opens docs in the app, ignoring the at:// url", () => {
  const space = "at://did:web:a.example/space/network.habitat.docs/abc";
  expect(
    resultLink({
      ...base,
      url: `${space}/did:web:a.example/network.habitat.docs.markdown/r1`,
      space,
      collection: "network.habitat.docs.markdown",
    }),
  ).toEqual({ kind: "doc", docId: space });
});

it("links http(s) urls externally and drops other schemes", () => {
  expect(resultLink({ ...base, url: "https://x.test/a" })).toEqual({
    kind: "external",
    href: "https://x.test/a",
  });
  expect(resultLink({ ...base, url: "at://did:web:a/b/c" })).toBeNull();
  expect(resultLink({ ...base, url: "javascript:alert(1)" })).toBeNull();
  expect(resultLink(base)).toBeNull();
});
