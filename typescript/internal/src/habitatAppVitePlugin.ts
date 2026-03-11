import generateFile from "vite-plugin-generate-file";
import clientMetadata from "./clientMetadata";
import type { Plugin } from "vite";
import util from "node:util";
import tailwindcss from "@tailwindcss/vite";
import { devtools } from "@tanstack/devtools-vite";

const cliArgs = util.parseArgs({
  args: process.argv.slice(process.argv.indexOf("--") + 1),
  options: {
    domain: {
      type: "string",
      default: process.env.DOMAIN ?? "frontend.habitat",
    },
    habitatDomain: {
      type: "string",
      default: process.env.HABITAT_DOMAIN,
    },
    outDir: {
      type: "string",
      default: process.env.OUT_DIR ?? "dist",
    },
  },
  allowPositionals: true,
});

export default function habitatAppPlugin(options?: {
  name?: string;
  domain?: string;
  habitatDomain?: string;
  hashRouting?: boolean;
}): Plugin[] {
  const domain = options?.domain ?? cliArgs.values.domain;
  const habitatDomain = options?.habitatDomain ?? cliArgs.values.habitatDomain;
  const hashRouting = options?.hashRouting ?? !!process.env.HASH_ROUTING;

  return [
    ...devtools({ eventBusConfig: { port: parseInt(process.env.DEVTOOLS_PORT ?? "42069", 10) } }),
    ...tailwindcss(),
    {
      name: "habitat-app-config",
      config() {
        return {
          define: {
            __DOMAIN__: domain ? `'${domain}'` : "undefined",
            __HABITAT_DOMAIN__: habitatDomain
              ? `'${habitatDomain}'`
              : "undefined",
            __HASH_ROUTING__: hashRouting ? "true" : "false",
          },
          base: domain ? `https://${domain}/` : "/",
          server: {
            host: true,
            allowedHosts: [".ts.net"],
            port: process.env.SERVER_PORT ? parseInt(process.env.SERVER_PORT, 10) : undefined,
          },
          build: {
            outDir: cliArgs.values.outDir,
          },
        };
      },
    },
    generateFile({
      data: clientMetadata(options?.name ?? "habitat", domain),
      output: "client-metadata.json",
    }),
  ];
}
