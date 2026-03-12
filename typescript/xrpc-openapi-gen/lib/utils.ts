import { readFile } from "node:fs/promises";
import type { LexiconDoc } from "@atproto/lexicon";

export async function loadLexicon(path: string): Promise<LexiconDoc> {
    const file = await readFile(path, "utf-8");
    const lexicon = JSON.parse(file) as LexiconDoc;
    return lexicon;
}

export function isEmptyObject(object: Record<string, unknown>) {
    return Object.keys(object).length === 0;
}

export function calculateTag(id: string): string {
    return id.split(".").slice(0, 3).join(".");
}

export enum Endpoint {
    Public,
    NeedsAuthentication,
    DoesNotExist,
}

export async function checkEndpoint(
    path: string,
    method = "GET",
): Promise<Endpoint> {
    const url = new URL(path, "https://habitat.network/xrpc/");
    const response = await fetch(url, { method });

    if (response.status === 401) {
        return Endpoint.NeedsAuthentication;
    }

    if (response.status === 404) {
        return Endpoint.DoesNotExist;
    }

    return Endpoint.Public;
}
