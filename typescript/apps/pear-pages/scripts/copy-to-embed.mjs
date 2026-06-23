// Copies the pre-rendered static output produced by `vite build` into the Go
// package that embeds it (internal/webui/dist). This runs as part of
// `pear-pages:build`, which `pear:build` depends on, so the Go binary always
// embeds an up-to-date UI.
//
// TanStack Start writes its static client + pre-rendered HTML to
// `.output/public`. We mirror that into the embed dir while preserving the
// committed `.gitkeep`/`.gitignore` placeholders that keep `//go:embed` happy
// when the UI has not been built yet.
import { cp, rm, readdir, mkdir, stat } from "node:fs/promises";
import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const appDir = dirname(dirname(fileURLToPath(import.meta.url)));
const candidates = [
  join(appDir, ".output", "public"),
  join(appDir, "dist"),
];
const dest = join(appDir, "..", "..", "..", "internal", "webui", "dist");

const src = candidates.find((p) => existsSync(p));
if (!src) {
  console.error(
    `pear-pages: could not find build output in any of:\n  ${candidates.join("\n  ")}`,
  );
  process.exit(1);
}

await mkdir(dest, { recursive: true });

// Remove everything in dest except the committed placeholders.
const keep = new Set([".gitkeep", ".gitignore"]);
for (const entry of await readdir(dest)) {
  if (keep.has(entry)) continue;
  await rm(join(dest, entry), { recursive: true, force: true });
}

// Copy the build output (including dotfiles / underscore-prefixed asset dirs).
for (const entry of await readdir(src)) {
  const from = join(src, entry);
  const to = join(dest, entry);
  const s = await stat(from);
  await cp(from, to, { recursive: s.isDirectory() });
}

console.log(`pear-pages: embedded static UI from ${src} -> ${dest}`);
