import { defineConfig, type UserConfig } from "vite";
import generateFile from "vite-plugin-generate-file";
import tailwindcss from "@tailwindcss/vite";
import { devtools } from "@tanstack/devtools-vite";
import clientMetadata from "./internal/src/clientMetadata";

// Shared base Vite config for Habitat apps. Merge into an app's config with
// `mergeConfig(habitatAppConfig({ name }), defineConfig({ ... }))`.
export default function habitatAppConfig(options?: {
  name?: string;
}): UserConfig {
  process.env.VITE_BASE_URL = process.env.CF_PAGES_URL || process.env.VITE_BASE_URL;
  return defineConfig({
    base: process.env.VITE_BASE_URL,
    server: {
      host: true,
      allowedHosts: [".ts.net", ".local.habitat.network"],
      port: process.env.SERVER_PORT
        ? parseInt(process.env.SERVER_PORT, 10)
        : undefined,
    },
    build: {
      outDir: process.env.OUT_DIR,
    },
    plugins: [
      ...devtools({
        eventBusConfig: {
          port: parseInt(process.env.DEVTOOLS_PORT ?? "42069", 10),
        },
      }),
      ...tailwindcss(),
      generateFile({
        data: clientMetadata(
          options?.name ?? "habitat",
          process.env.VITE_BASE_URL ?? "",
        ),
        output: "client-metadata.json",
      }),
    ],
  });
}
