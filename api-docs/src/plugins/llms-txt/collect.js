const fs = require("node:fs");
const path = require("node:path");

// Ordered as a developer should read them; mirrors the /docs/ landing page.
// `dir` is relative to docs/; "" is the docs root itself.
const SECTIONS = [
  { dir: "", label: "Start here" },
  { dir: "space-proxy", label: "Quickstart: identity and OAuth" },
  { dir: "guides", label: "Guides" },
  { dir: "building", label: "Building on Habitat" },
  { dir: "rebac", label: "Relationship-based access control" },
  { dir: "arch", label: "Architecture" },
  { dir: "opensocial", label: "Opensocial" },
];

// Frontmatter here is uniformly `key: value`, so a full YAML parser would be
// a dependency for no gain.
function parseFrontMatter(raw) {
  const match = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?/.exec(raw);
  if (!match) return { data: {}, body: raw };
  const data = {};
  for (const line of match[1].split(/\r?\n/)) {
    const idx = line.indexOf(":");
    if (idx === -1) continue;
    data[line.slice(0, idx).trim()] = line.slice(idx + 1).trim();
  }
  return { data, body: raw.slice(match[0].length) };
}

function walk(dir) {
  const out = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    // docs/api is generated from the OpenAPI spec; it is linked from
    // llms.txt as a whole rather than inlined page by page.
    if (entry.isDirectory()) {
      if (entry.name === "api") continue;
      out.push(...walk(full));
    } else if (entry.name.endsWith(".mdx") || entry.name.endsWith(".md")) {
      out.push(full);
    }
  }
  return out;
}

function collectDocs(docsDir) {
  return walk(docsDir)
    .map((file) => {
      const { data, body } = parseFrontMatter(fs.readFileSync(file, "utf8"));
      if (!data.slug) return null;
      const heading = /^#\s+(.+)$/m.exec(body);
      const rel = path.relative(docsDir, file);
      const dir = path.dirname(rel);
      return {
        slug: data.slug,
        title: heading ? heading[1].trim() : data.sidebar_label || data.slug,
        section: dir === "." ? "" : dir,
        body: body.trim(),
      };
    })
    .filter(Boolean);
}

function docUrl(baseUrl, slug) {
  return `${baseUrl}/docs${slug === "/" ? "" : slug}`;
}

function grouped(pages) {
  return SECTIONS.map((section) => ({
    label: section.label,
    pages: pages
      .filter((p) => p.section === section.dir)
      .sort((a, b) => a.slug.localeCompare(b.slug)),
  })).filter((section) => section.pages.length > 0);
}

function renderIndex(pages, baseUrl) {
  const lines = [
    "# Habitat",
    "",
    "> Habitat is an open-source, self-hostable data ownership layer for",
    "> organizations, built on AT Protocol primitives: identities,",
    "> user-owned repositories, fine-grained permissions, and syncing.",
    "",
  ];
  for (const section of grouped(pages)) {
    lines.push(`## ${section.label}`, "");
    for (const page of section.pages) {
      lines.push(`- [${page.title}](${docUrl(baseUrl, page.slug)})`);
    }
    lines.push("");
  }
  lines.push(
    "## Reference",
    "",
    `- [HTTP API reference](${baseUrl}/docs/api)`,
    `- [OpenAPI specification](${baseUrl}/openapi.json)`,
    `- [Full documentation as one file](${baseUrl}/llms-full.txt)`,
    "",
  );
  return lines.join("\n");
}

function renderFull(pages, baseUrl) {
  const chunks = [];
  for (const section of grouped(pages)) {
    for (const page of section.pages) {
      chunks.push(
        `# ${page.title}`,
        `Source: ${docUrl(baseUrl, page.slug)}`,
        "",
        page.body,
        "",
        "---",
        "",
      );
    }
  }
  return chunks.join("\n");
}

module.exports = { collectDocs, renderIndex, renderFull };
