import { defineConfig, mergeConfig } from "vite";
import viteReact from "@vitejs/plugin-react";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import habitatAppConfig from "../../vite.config.app";

export default mergeConfig(
  habitatAppConfig({ name: "Habitat Docs" }),
  defineConfig({
    server: {
      hmr: false, // creates multiple libp2p nodes
    },
    resolve: {
      tsconfigPaths: true,
    },
    plugins: [tanstackRouter(), viteReact()],
  }),
);
