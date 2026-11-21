// Apps: code a member uploads, which the app runs in a sandboxed Dynamic
// Worker at its own URL. What the browser and the server both need to know
// about an app's bundle and its run URL lives here.

// The bundle is stored as one blob in pear, which refuses bodies over 5 MiB
// (see lib/files.ts). The JSON wrapper adds a little, so the sources may
// total a bit less.
export const MAX_BUNDLE_BYTES = 5 * 1024 * 1024 - 64 * 1024;
export const MAX_MODULES = 200;

// The Workers compatibility date new apps run with.
export const DEFAULT_COMPATIBILITY_DATE = "2026-08-01";

// A bundle is the app's modules by path. Each path's extension sets its
// module type (see moduleOf in server/apps/runner.ts).
export interface AppBundle {
  modules: Record<string, string>;
}

// MODULE_EXTENSIONS are the files an upload keeps. .js and .mjs are ES
// modules, .cjs CommonJS, .json JSON; the rest are text modules the code
// can import (HTML, CSS, Markdown, ...). Binary files (WebAssembly, images)
// aren't supported yet: the bundle is text.
export const MODULE_EXTENSIONS = [
  ".js",
  ".mjs",
  ".cjs",
  ".json",
  ".txt",
  ".html",
  ".css",
  ".md",
  ".svg",
] as const;

export function isModulePath(path: string): boolean {
  const lower = path.toLowerCase();
  return MODULE_EXTENSIONS.some((ext) => lower.endsWith(ext));
}

export function isScriptPath(path: string): boolean {
  return /\.(m?js|cjs)$/i.test(path);
}

// slugify makes a DNS-safe name from an app's display name: lowercase
// letters, digits and single hyphens, at most 40 characters.
export function slugify(name: string): string {
  const slug = name
    .normalize("NFKD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40)
    .replace(/-+$/g, "");
  return slug || "app";
}

const SLUG = /^[a-z0-9](?:[a-z0-9-]{0,38}[a-z0-9])?$/;
const SKEY = /^[a-z0-9]{1,20}$/;

// hostLabel is the DNS label an app runs under: its slug and its space's
// key, so two apps with the same name in different groups still get
// different URLs. Returns undefined when either part isn't DNS-safe.
export function hostLabel(slug: string, skey: string): string | undefined {
  if (!SLUG.test(slug) || !SKEY.test(skey.toLowerCase())) return undefined;
  return `${slug}-${skey.toLowerCase()}`;
}

// runUrl is where an app is visited: its own subdomain of the run domain,
// so each app is its own origin and none shares one with the Habitat app.
// The scheme follows the app's own base URL (http in local dev).
export function runUrl(
  baseUrl: string,
  runDomain: string,
  host: string,
): string {
  const scheme = new URL(baseUrl).protocol;
  return `${scheme}//${host}.${runDomain}/`;
}

// hostnameOf drops a host's port: an app is found by its hostname alone,
// so "x.apps.localhost" matches a run domain of "apps.localhost:5181", and a
// proxy that leaves the default port off the Host header still matches.
function hostnameOf(host: string): string {
  return host.toLowerCase().replace(/:\d+$/, "");
}

// appHostOf returns the app label a request was sent to, or undefined when
// it isn't for an app on the run domain.
export function appHostOf(
  url: URL,
  runDomain: string | undefined,
): string | undefined {
  if (!runDomain) return undefined;
  const suffix = `.${hostnameOf(runDomain)}`;
  const host = hostnameOf(url.host);
  if (!host.endsWith(suffix)) return undefined;
  const label = host.slice(0, -suffix.length);
  return label && !label.includes(".") ? label : undefined;
}

// isRunRequest reports whether a request is for the run domain at all
// (an app's subdomain or the domain itself). The Habitat app is never
// served there.
export function isRunRequest(url: URL, runDomain: string | undefined): boolean {
  if (!runDomain) return false;
  const host = hostnameOf(url.host);
  const domain = hostnameOf(runDomain);
  return host === domain || host.endsWith(`.${domain}`);
}

// normalizePath turns an uploaded file's path into a module path: forward
// slashes, no leading "./" or "/", and no "." or ".." segments.
export function normalizePath(path: string): string | undefined {
  const parts = path.replace(/\\/g, "/").split("/");
  const out: string[] = [];
  for (const part of parts) {
    if (part === "" || part === ".") continue;
    if (part === "..") return undefined;
    out.push(part);
  }
  return out.length > 0 ? out.join("/") : undefined;
}

// stripCommonRoot drops a folder every path shares, so uploading a folder
// "my-app/" gives "index.js" rather than "my-app/index.js".
export function stripCommonRoot(paths: string[]): string[] {
  if (paths.length === 0 || paths.some((p) => !p.includes("/"))) return paths;
  const root = paths[0].split("/")[0];
  if (!paths.every((p) => p.startsWith(`${root}/`))) return paths;
  return stripCommonRoot(paths.map((p) => p.slice(root.length + 1)));
}

// guessEntrypoint picks the main module of an upload: a conventional name
// when there is one, else the only script.
export function guessEntrypoint(paths: string[]): string | undefined {
  for (const name of [
    "index.js",
    "index.mjs",
    "worker.js",
    "worker.mjs",
    "main.js",
    "src/index.js",
    "dist/index.js",
  ]) {
    if (paths.includes(name)) return name;
  }
  const scripts = paths.filter(isScriptPath);
  return scripts.length === 1 ? scripts[0] : scripts.sort()[0];
}

export class InvalidBundle extends Error {}

// validateBundle checks an upload before it is stored: module paths,
// types, count and total size, and that the entrypoint is one of its ES
// modules. Returns the bundle's size as stored.
export function validateBundle(bundle: AppBundle, entrypoint: string): number {
  const paths = Object.keys(bundle.modules);
  if (paths.length === 0) throw new InvalidBundle("Upload at least one file");
  if (paths.length > MAX_MODULES) {
    throw new InvalidBundle(`An app can have at most ${MAX_MODULES} files`);
  }
  for (const path of paths) {
    if (normalizePath(path) !== path) {
      throw new InvalidBundle(`"${path}" isn't a valid module path`);
    }
    if (!isModulePath(path)) {
      throw new InvalidBundle(
        `"${path}" isn't a supported file type (${MODULE_EXTENSIONS.join(", ")})`,
      );
    }
    if (typeof bundle.modules[path] !== "string") {
      throw new InvalidBundle(`"${path}" isn't text`);
    }
    if (/\.json$/i.test(path)) {
      try {
        JSON.parse(bundle.modules[path]);
      } catch {
        throw new InvalidBundle(`"${path}" isn't valid JSON`);
      }
    }
  }
  if (!(entrypoint in bundle.modules)) {
    throw new InvalidBundle(
      `The entrypoint "${entrypoint}" isn't in the upload`,
    );
  }
  if (!/\.m?js$/i.test(entrypoint)) {
    throw new InvalidBundle("The entrypoint must be a .js or .mjs ES module");
  }
  const size = new TextEncoder().encode(JSON.stringify(bundle)).byteLength;
  if (size > MAX_BUNDLE_BYTES) {
    throw new InvalidBundle("The upload is too large (5 MB at most)");
  }
  return size;
}
