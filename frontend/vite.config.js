import { defineConfig } from "vite";
import viteReact from "@vitejs/plugin-react";
import { TanStackRouterVite } from "@tanstack/router-plugin/vite";
import { resolve } from "node:path";
import generateFile from "vite-plugin-generate-file";

import clientMetadata from "./client-metadata";

const domain = process.env.DOMAIN;
const habitatDomain = process.env.HABITAT_DOMAIN;

// https://vitejs.dev/config/
export default defineConfig({
  define: {
    __DOMAIN__: domain ? `'${domain}'` : "undefined",
    __HABITAT_DOMAIN__: habitatDomain ? `'${habitatDomain}'` : "undefined",
    __HASH_ROUTING__: process.env.HASH_ROUTING ? "true" : "false",
  },
  plugins: [
    TanStackRouterVite({ autoCodeSplitting: true }),
    viteReact(),
    generateFile({
      data: clientMetadata(domain),
      output: "client-metadata.json",
    }),
  ],
  test: {
    globals: true,
    environment: "jsdom",
  },
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
