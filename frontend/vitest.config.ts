import { defineConfig } from "vitest/config";
import { resolve } from "node:path";

export default defineConfig({
  resolve: {
    alias: {
      "@": resolve(__dirname, "./src"),
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    env: {
      VITE_BASE_URL: "https://app.test/",
      VITE_HABITAT_DOMAIN: "pear.test",
    },
  },
});
