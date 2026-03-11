import { defineConfig } from "vite";
import { devtools } from "@tanstack/devtools-vite";
import viteReact from "@vitejs/plugin-react";
import viteTsConfigPaths from "vite-tsconfig-paths";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import habitatPlugins from "internal/habitatAppVitePlugin";

const config = defineConfig({
  server: {
    hmr: false, // creates multiple libp2p nodes
  },
  plugins: [
    devtools({
      enhancedLogs: { enabled: false },
      eventBusConfig: { port: parseInt(process.env.DEVTOOLS_PORT ?? "42069", 10) },
    }),
    // this is the plugin that enables path aliases
    viteTsConfigPaths({
      projects: ["./tsconfig.json"],
    }),
    tanstackRouter(),
    viteReact(),
    ...habitatPlugins(),
  ],
});

export default config;
