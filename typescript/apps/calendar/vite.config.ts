import { defineConfig } from "vite";
import viteReact from "@vitejs/plugin-react";
import viteTsConfigPaths from "vite-tsconfig-paths";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import habitatPlugins from "internal/habitatAppVitePlugin";

const calendarDomain = process.env.CALENDAR_DOMAIN;

const config = defineConfig({
  plugins: [
    viteTsConfigPaths({
      projects: ["./tsconfig.json"],
    }),
    tanstackRouter(),
    viteReact(),
    ...habitatPlugins({
      name: "Habitat Calendar",
    }),
  ],
  define: {
    __CALENDAR_DOMAIN__: calendarDomain ? `'${calendarDomain}'` : "undefined",
  },
});

export default config;
