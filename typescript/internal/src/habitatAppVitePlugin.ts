import generateFile from "vite-plugin-generate-file";
import clientMetadata from "./clientMetadata.ts";
import type { Plugin } from "vite";

export default function habitatAppPlugin(options?: {
  domain?: string;
  habitatDomain?: string;
  hashRouting?: boolean;
}): Plugin[] {
  const domain = options?.domain ?? process.env.DOMAIN ?? "frontend.habitat";
  const habitatDomain = options?.habitatDomain ?? process.env.HABITAT_DOMAIN;
  const hashRouting = options?.hashRouting ?? !!process.env.HASH_ROUTING;

  return [
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
          },
        };
      },
    },
    generateFile({
      data: clientMetadata(domain),
      output: "client-metadata.json",
    }),
  ];
}
