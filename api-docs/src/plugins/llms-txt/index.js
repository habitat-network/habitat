const fs = require("node:fs");
const path = require("node:path");
const { collectDocs, renderIndex, renderFull } = require("./collect");

// Emits agent-readable copies of the docs into the build output, so they
// ship with the site and cannot drift from it.
module.exports = function llmsTxtPlugin(context) {
  return {
    name: "habitat-llms-txt",
    async postBuild({ outDir, siteConfig }) {
      const baseUrl = siteConfig.url.replace(/\/$/, "");
      const pages = collectDocs(path.join(context.siteDir, "docs"));

      fs.writeFileSync(
        path.join(outDir, "llms.txt"),
        renderIndex(pages, baseUrl),
      );
      fs.writeFileSync(
        path.join(outDir, "llms-full.txt"),
        renderFull(pages, baseUrl),
      );

      // Serve the OpenAPI spec the reference is rendered from, so an agent
      // can consume the API surface directly instead of scraping HTML.
      fs.copyFileSync(
        path.join(
          context.siteDir,
          "..",
          "typescript",
          "xrpc-openapi-gen",
          "spec",
          "api.json",
        ),
        path.join(outDir, "openapi.json"),
      );
    },
  };
};
