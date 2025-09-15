import { defineConfig } from "vite";
import viteReact from "@vitejs/plugin-react";
import { TanStackRouterVite } from "@tanstack/router-plugin/vite";
import { resolve } from "node:path";
import generateFile from 'vite-plugin-generate-file'

import clientMetadata from './client-metadata'

const domain = process.env.DOMAIN

// https://vitejs.dev/config/
export default defineConfig({
  define: {
    __DOMAIN__: `'${domain}'`
  },
  plugins: [
    TanStackRouterVite({ autoCodeSplitting: true }),
    viteReact(),
    generateFile({
      data: clientMetadata(domain),
      output: 'client-metadata.json'
    })
  ],
  test: {
    globals: true,
    environment: "jsdom",
  },
  resolve: {
    alias: {
      '@': resolve(__dirname, './src'),
    },
  },
  server: {
    host: true,
    allowedHosts: [".ts.net"]
  },
});
