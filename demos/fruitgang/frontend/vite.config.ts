import { defineConfig } from "vite";
import viteReact from "@vitejs/plugin-react";
import viteTsConfigPaths from "vite-tsconfig-paths";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import habitatPlugins from "internal/habitatAppVitePlugin";

export default defineConfig({
  plugins: [
    viteTsConfigPaths({ projects: ["./tsconfig.json"] }),
    tanstackRouter(),
    viteReact(),
    ...habitatPlugins({ name: "Fruit Gang" }),
    {
      name: "fruitgang-api-config",
      config() {
        return {
          define: {
            __FRUITGANG_API__: JSON.stringify(
              process.env.FRUITGANG_API ?? "https://fruitgang-api.local.habitat.network"
            ),
          },
        };
      },
    },
  ],
});
