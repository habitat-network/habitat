import { defineConfig } from "vite";
import viteReact from "@vitejs/plugin-react";
import viteTsConfigPaths from "vite-tsconfig-paths";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import habitatPlugins from "internal/habitatAppVitePlugin";

const config = defineConfig({
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
    ...habitatPlugins({ name: "Habitat Docs" }),
  ],
});

export default config;
