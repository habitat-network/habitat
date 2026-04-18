import { writeFile, mkdir } from "node:fs/promises";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import fg from "fast-glob";
import { calculateTag, loadLexicon } from "./lib/utils.ts";
import {
    convertArray,
    convertObject,
    convertProcedure,
    convertQuery,
    convertRecord,
    convertString,
    convertToken,
} from "./lib/converters/mod.ts";

import type { OpenAPIV3_1 } from "openapi-types";

const __dirname = dirname(fileURLToPath(import.meta.url));

const lexiconPaths = await fg("../../lexicons/network/**/*.json", {
    cwd: __dirname,
    absolute: true,
});

const paths: OpenAPIV3_1.PathsObject = {};
const components: OpenAPIV3_1.ComponentsObject = {
    schemas: {},
    securitySchemes: {
        Bearer: {
            type: "http",
            scheme: "bearer",
        },
    },
};
const tagNames = new Set<string>();

for (const lexiconPath of lexiconPaths) {
    const doc = await loadLexicon(lexiconPath);

    const id = doc.id;
    const defs = doc.defs;

    console.info(id);
    tagNames.add(calculateTag(id));

    for (const [name, def] of Object.entries(defs)) {
        const identifier = name === "main" ? id : `${id}.${name}`;

        switch (def.type) {
            case "array":
                components.schemas![identifier] = convertArray(id, name, def);
                break;
            case "object":
                components.schemas![identifier] = convertObject(id, name, def);
                break;
            case "procedure": {
                const post = await convertProcedure(id, name, def);

                if (post) {
                    // @ts-ignore FIXME: Also confused about ArraySchemaObject
                    paths[`/${id}`] = { post };
                }
                break;
            }
            case "query": {
                const get = await convertQuery(id, name, def);

                if (get) {
                    // @ts-ignore FIXME: Also confused about ArraySchemaObject
                    paths[`/${id}`] = { get };
                }
                break;
            }
            case "record":
                components.schemas![identifier] = convertRecord(id, name, def);
                break;
            case "string":
                components.schemas![identifier] = convertString(id, name, def);
                break;
            case "subscription":
                // No way to represent this in OpenAPI
                break;
            case "token":
                components.schemas![identifier] = convertToken(id, name, def);
                break;
            default:
                throw new Error(`Unknown type: ${def.type}`);
        }
    }
}

const api: OpenAPIV3_1.Document = {
    openapi: "3.1.0",
    info: {
        title: "habitat's API",
        summary: "OpenAPI spec generated from habitat's ATProtocol lexicons.",
        description: "Welcome to the habitat API reference! We are currently under development and our API may make breaking changes without notice. Stay tuned [@habitat.network](https://bsky.app/profile/habitat.network) on Bluesky and [habitat.leaflet.pub](https://habitat.leaflet.pub) for future formal releases.",
        version: "0.0.0",
        license: {
            name: "MIT License",
            identifier: "MIT",
        },
    },
    servers: [
        {
            url: "https://habitat-953995456319.us-west1.run.app/xrpc/", // TODO: should this be hitting dev server locally for generation from a given branch?
            description: "Habitat XRPC server",
        },
    ],
    paths,
    components,
    tags: Array.from(tagNames).map((name) => ({ name })),
};

const specDir = resolve(__dirname, "spec");
await mkdir(specDir, { recursive: true });
await writeFile(resolve(specDir, "api.json"), JSON.stringify(api, null, 2) + "\n");
console.info("Wrote spec/api.json");
