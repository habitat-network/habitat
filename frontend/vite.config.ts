import { defineConfig } from "vite";
import viteReact from "@vitejs/plugin-react";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import { resolve } from "node:path";
import habitatAppPlugin from "internal/habitatAppVitePlugin.ts";

const domain = process.env.DOMAIN ?? "frontend.habitat";

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [
    tanstackRouter({ autoCodeSplitting: true }),
    viteReact(),
    ...habitatAppPlugin(),
  ],
  resolve: {
    alias: {
      "@": resolve(__dirname, "./src"),
    },
  },
  server: {
    host: true,
    allowedHosts: [".ts.net"],
  },
  base: domain ? `https://${domain}/` : "/",
});
