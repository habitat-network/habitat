import { defineConfig, mergeConfig } from "vite";
import viteReact from "@vitejs/plugin-react";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import { resolve } from "node:path";
import habitatAppConfig from "../typescript/vite.config.app";

// https://vitejs.dev/config/
export default mergeConfig(
  habitatAppConfig({ name: "Habitat" }),
  defineConfig({
    plugins: [tanstackRouter({ autoCodeSplitting: true }), viteReact()],
    resolve: {
      alias: {
        "@": resolve(__dirname, "./src"),
      },
    },
    server: {
      port: 6000,
    },
  }),
);
