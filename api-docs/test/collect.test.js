const test = require("node:test");
const assert = require("node:assert");
const path = require("node:path");
const {
  collectDocs,
  renderIndex,
  renderFull,
} = require("../src/plugins/llms-txt/collect");

const DOCS_DIR = path.join(__dirname, "..", "docs");

test("collectDocs reads every hand-written page and skips generated ones", () => {
  const pages = collectDocs(DOCS_DIR);
  assert.ok(pages.length >= 20, `expected >=20 pages, got ${pages.length}`);
  assert.ok(
    pages.every((p) => p.section !== "api"),
    "generated docs/api pages must be excluded",
  );
});

test("collectDocs never emits a slug with a .mdx extension", () => {
  for (const page of collectDocs(DOCS_DIR)) {
    assert.ok(!page.slug.endsWith(".mdx"), `slug leaked .mdx: ${page.slug}`);
  }
});

test("collectDocs extracts the H1 as the title", () => {
  const pages = collectDocs(DOCS_DIR);
  const habitat = pages.find((p) => p.slug === "/habitat");
  assert.ok(habitat, "expected a page at /habitat");
  assert.equal(habitat.title, "What is Habitat?");
});

test("collectDocs strips frontmatter from the body", () => {
  for (const page of collectDocs(DOCS_DIR)) {
    assert.ok(!page.body.startsWith("---"), `frontmatter left in ${page.slug}`);
  }
});

test("renderIndex emits absolute URLs under the canonical origin", () => {
  const out = renderIndex(collectDocs(DOCS_DIR), "https://api.habitat.network");
  assert.match(out, /^# Habitat/m);
  assert.match(
    out,
    /https:\/\/api\.habitat\.network\/docs\/space-proxy\/getting-started/,
  );
  assert.ok(!/\.mdx\)/.test(out), "index must not link to .mdx URLs");
});

test("renderFull inlines page prose", () => {
  const out = renderFull(collectDocs(DOCS_DIR), "https://api.habitat.network");
  assert.ok(out.length > 5000, `expected substantial prose, got ${out.length}`);
  assert.match(out, /Organizational Data Server|data ownership layer/);
});
