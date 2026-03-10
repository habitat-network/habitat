import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import dts from "vite-plugin-dts";
import path from "path";

export default defineConfig({
  plugins: [
    react(),
    dts({
      include: ["src/habitatAppVitePlugin.ts", "src/clientMetadata.ts"], // Include dependencies
      entryRoot: "src",
      rollupTypes: false, // Don't rollup since we're only building the plugin
      copyDtsFiles: false,
    }),
  ],
  build: {
    lib: {
      entry: {
        habitatAppVitePlugin: path.resolve(
          __dirname,
          "src/habitatAppVitePlugin.ts",
        ),
      },
      formats: ["es"],
    },
    rollupOptions: {
      external: [
        // React and related
        "react",
        "react-dom",
        "react/jsx-runtime",

        // Peer dependencies
        "@tanstack/react-query",
        "react-hook-form",

        // Regular dependencies (externalized to avoid bundling)
        "@atproto/api",
        "@atproto/identity",
        "@atproto/oauth-client-browser",
        "@base-ui/react",
        "@uidotdev/usehooks",
        "jose",
        "openid-client",
        "web-vitals",
        "zustand",
        "zustand/middleware",

        // Workspace dependency
        "api",

        // Plugin dependencies
        "vite-plugin-generate-file",
        "@tailwindcss/vite",

        // Node.js built-ins (for the Vite plugin)
        "node:util",
        "util",
      ],
      output: {
        preserveModules: false,
        preserveModulesRoot: "src",
      },
    },
    outDir: "dist",
    sourcemap: true,
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
    },
  },
});
