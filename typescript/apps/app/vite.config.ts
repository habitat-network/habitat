import { defineConfig } from "vite";
import { devtools } from "@tanstack/devtools-vite";

import { tanstackStart } from "@tanstack/react-start/plugin/vite";

import viteReact from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { cloudflare } from "@cloudflare/vite-plugin";

const config = defineConfig({
  resolve: { tsconfigPaths: true },
  server: {
    host: true,
    allowedHosts: [".ts.net", ".local.habitat.network"],
  },
  // The e2e test bed (e2e/compose.yaml) serves the build at chalk.habitat-e2e.dev.
  preview: {
    allowedHosts: [".ts.net", ".local.habitat.network", ".habitat-e2e.dev"],
  },
  plugins: [
    devtools(),
    cloudflare({ viteEnvironment: { name: "ssr" } }),
    tailwindcss(),
    tanstackStart(),
    viteReact(),
  ],
});

export default config;
