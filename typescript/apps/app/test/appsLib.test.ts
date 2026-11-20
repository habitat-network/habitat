import { describe, expect, it } from "vitest";
import {
  appHostOf,
  guessEntrypoint,
  hostLabel,
  InvalidBundle,
  isRunRequest,
  MAX_BUNDLE_BYTES,
  normalizePath,
  runUrl,
  slugify,
  stripCommonRoot,
  validateBundle,
} from "@/lib/apps";

describe("slugify", () => {
  it("makes a DNS-safe name", () => {
    expect(slugify("My Café App!")).toBe("my-cafe-app");
    expect(slugify("  --Hello__World--  ")).toBe("hello-world");
    expect(slugify("🚀")).toBe("app");
  });

  it("caps the length without a trailing hyphen", () => {
    const slug = slugify(`${"a".repeat(39)} b`);
    expect(slug.length).toBeLessThanOrEqual(40);
    expect(slug.endsWith("-")).toBe(false);
  });
});

describe("hostLabel", () => {
  it("joins the slug and space key", () => {
    expect(hostLabel("chat", "3kabc234")).toBe("chat-3kabc234");
  });

  it("refuses parts that aren't DNS-safe", () => {
    expect(hostLabel("Chat", "3kabc")).toBeUndefined();
    expect(hostLabel("chat", "3k.abc")).toBeUndefined();
    expect(hostLabel("-chat", "3kabc")).toBeUndefined();
  });
});

describe("run domain routing", () => {
  const DOMAIN = "apps.habitat.network";

  it("finds the app label of a run URL", () => {
    expect(
      appHostOf(new URL("https://chat-3k.apps.habitat.network/x"), DOMAIN),
    ).toBe("chat-3k");
  });

  it("ignores the port on either side", () => {
    expect(
      appHostOf(
        new URL("https://chat-3k.apps.localhost:5181/"),
        "apps.localhost:5181",
      ),
    ).toBe("chat-3k");
    expect(
      appHostOf(
        new URL("https://chat-3k.apps.local.habitat.network/"),
        "apps.local.habitat.network:443",
      ),
    ).toBe("chat-3k");
  });

  it("finds no app on the run domain itself, deeper names, or other hosts", () => {
    expect(
      appHostOf(new URL("https://apps.habitat.network/"), DOMAIN),
    ).toBeUndefined();
    expect(
      appHostOf(new URL("https://a.b.apps.habitat.network/"), DOMAIN),
    ).toBeUndefined();
    expect(
      appHostOf(new URL("https://app.habitat.network/"), DOMAIN),
    ).toBeUndefined();
    expect(
      appHostOf(new URL("https://evilapps.habitat.network/"), DOMAIN),
    ).toBeUndefined();
    expect(
      appHostOf(new URL("https://x.apps.habitat.network/"), undefined),
    ).toBeUndefined();
  });

  it("sends the whole run domain, and only it, to the runner", () => {
    expect(isRunRequest(new URL("https://apps.habitat.network/"), DOMAIN)).toBe(
      true,
    );
    expect(
      isRunRequest(new URL("https://a.b.apps.habitat.network/"), DOMAIN),
    ).toBe(true);
    expect(isRunRequest(new URL("https://app.habitat.network/"), DOMAIN)).toBe(
      false,
    );
    expect(isRunRequest(new URL("https://app.habitat.network/"), "")).toBe(
      false,
    );
  });

  it("builds run URLs with the app's own scheme", () => {
    expect(runUrl("https://app.habitat.network", DOMAIN, "chat-3k")).toBe(
      "https://chat-3k.apps.habitat.network/",
    );
    expect(
      runUrl("http://localhost:5181", "apps.localhost:5181", "chat-3k"),
    ).toBe("http://chat-3k.apps.localhost:5181/");
  });
});

describe("upload paths", () => {
  it("normalizes paths and refuses ones that climb out", () => {
    expect(normalizePath("./src\\index.js")).toBe("src/index.js");
    expect(normalizePath("/a//b.js")).toBe("a/b.js");
    expect(normalizePath("../secret.js")).toBeUndefined();
    expect(normalizePath("")).toBeUndefined();
  });

  it("drops a folder every path shares", () => {
    expect(stripCommonRoot(["my-app/index.js", "my-app/lib/a.js"])).toEqual([
      "index.js",
      "lib/a.js",
    ]);
    expect(stripCommonRoot(["a/b/index.js", "a/b/c.js"])).toEqual([
      "index.js",
      "c.js",
    ]);
    expect(stripCommonRoot(["index.js", "lib/a.js"])).toEqual([
      "index.js",
      "lib/a.js",
    ]);
  });

  it("guesses the entrypoint", () => {
    expect(guessEntrypoint(["lib.js", "index.js"])).toBe("index.js");
    expect(guessEntrypoint(["app.mjs", "data.json"])).toBe("app.mjs");
    expect(guessEntrypoint(["data.json"])).toBeUndefined();
  });
});

describe("validateBundle", () => {
  const ok = { modules: { "index.js": "export default {}", "a.json": "{}" } };

  it("accepts a bundle and returns its stored size", () => {
    expect(validateBundle(ok, "index.js")).toBeGreaterThan(0);
  });

  it.each([
    [{ modules: {} }, "index.js", /at least one/],
    [ok, "missing.js", /isn't in the upload/],
    [ok, "a.json", /must be a .js or .mjs/],
    [
      { modules: { "index.js": "", "img.png": "" } },
      "index.js",
      /supported file type/,
    ],
    [
      { modules: { "index.js": "", "../x.js": "" } },
      "index.js",
      /valid module path/,
    ],
    [{ modules: { "index.js": "", "a.json": "{" } }, "index.js", /valid JSON/],
    [
      { modules: { "index.js": "x".repeat(MAX_BUNDLE_BYTES) } },
      "index.js",
      /too large/,
    ],
  ])("refuses a bad upload (%#)", (bundle, entrypoint, message) => {
    expect(() => validateBundle(bundle, entrypoint)).toThrow(InvalidBundle);
    expect(() => validateBundle(bundle, entrypoint)).toThrow(message);
  });
});
