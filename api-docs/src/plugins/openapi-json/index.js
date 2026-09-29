const fs = require("node:fs");
const path = require("node:path");

// Serve the OpenAPI spec the reference is rendered from, so an agent can
// consume the API surface directly instead of scraping HTML.
module.exports = function openapiJsonPlugin(context) {
  return {
    name: "habitat-openapi-json",
    async postBuild({ outDir }) {
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
