import { defineConfig } from "vite";
import viteReact from "@vitejs/plugin-react";
import viteTsConfigPaths from "vite-tsconfig-paths";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import habitatPlugins from "internal/habitatAppVitePlugin";

const config = defineConfig({
  plugins: [
    viteTsConfigPaths({
      projects: ["./tsconfig.json"],
    }),
    tanstackRouter(),
    viteReact(),
    ...habitatPlugins({ name: "Greensky" }),
  ],
});

export default config;
