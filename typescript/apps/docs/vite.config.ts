import { defineConfig, mergeConfig } from "vite";
import viteReact from "@vitejs/plugin-react";
import viteTsConfigPaths from "vite-tsconfig-paths";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import habitatAppConfig from "../../vite.config.app";

export default mergeConfig(
  habitatAppConfig({ name: "Habitat Docs" }),
  defineConfig({
    server: {
      hmr: false, // creates multiple libp2p nodes
    },
    plugins: [
      // this is the plugin that enables path aliases
      viteTsConfigPaths({
        projects: ["./tsconfig.json"],
      }),
      tanstackRouter(),
      viteReact(),
    ],
  }),
);
