import type { Agent, LexValue } from "@atproto/lex";
import { xrpc } from "@atproto/lex";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import { network } from "api";
import type { AppBundle } from "@/lib/apps";
import { deleteRecordIndexed, putRecordIndexed } from "../stream";
import { APP_COLLECTION } from "./sink";

// Calls the app makes against the Habitat instance to deploy Apps, each
// with the group's own session: an app belongs to the group, like a file.

// APP_SPACE_TYPE is the type of the space each deployed app lives in.
export const APP_SPACE_TYPE = "network.habitat.apps";

// The opensocial role every org member holds: naming it on an app space's
// access record makes the app's metadata and code readable by the whole
// group (see files.server.ts).
const MEMBER_ROLE = "member";

// createAppSpace creates a new space for one app under the org, readable
// by every org member. Must be called with the org's own session.
export async function createAppSpace(
  orgAgent: Agent,
  orgDid: DidString,
): Promise<SpaceRefString> {
  const { body } = await xrpc(orgAgent, network.habitat.opensocial.createSpace.main, {
    body: { org: orgDid, type: APP_SPACE_TYPE, roles: [MEMBER_ROLE] },
  });
  return body.uri as SpaceRefString;
}

// encodeBundle is the bytes a bundle is stored as. Keys are sorted so the
// same upload always makes the same blob (and so the same CID).
export function encodeBundle(bundle: AppBundle): Uint8Array {
  const modules: Record<string, string> = {};
  for (const path of Object.keys(bundle.modules).sort()) {
    modules[path] = bundle.modules[path];
  }
  return new TextEncoder().encode(JSON.stringify({ modules }));
}

// decodeBundle reads a stored bundle back.
export function decodeBundle(bytes: Uint8Array): AppBundle {
  const parsed = JSON.parse(new TextDecoder().decode(bytes)) as unknown;
  const modules = (parsed as { modules?: unknown })?.modules;
  if (!modules || typeof modules !== "object" || Array.isArray(modules)) {
    throw new Error("app bundle has no modules");
  }
  for (const value of Object.values(modules)) {
    if (typeof value !== "string") throw new Error("app bundle is malformed");
  }
  return { modules: modules as Record<string, string> };
}

export interface AppRecordInput {
  name: string;
  slug: string;
  description?: string;
  entrypoint: string;
  code: LexValue;
  version: number;
  compatibilityDate: string;
  createdBy?: string;
  updatedBy: DidString;
  createdAt?: string;
}

// putAppRecord writes an app's network.habitat.apps.app record into the
// org's repo in the app's space, and returns once the group's stream has
// indexed it into the apps table, so the runner serves the new version as
// soon as this returns.
export function putAppRecord(
  orgDid: DidString,
  space: SpaceRefString,
  app: AppRecordInput,
) {
  const now = new Date().toISOString();
  return putRecordIndexed(orgDid, orgDid, {
    space,
    repo: orgDid,
    collection: APP_COLLECTION,
    rkey: "self",
    record: {
      $type: APP_COLLECTION,
      name: app.name,
      slug: app.slug,
      ...(app.description ? { description: app.description } : {}),
      entrypoint: app.entrypoint,
      code: app.code,
      version: app.version,
      compatibilityDate: app.compatibilityDate,
      createdBy: app.createdBy ?? app.updatedBy,
      updatedBy: app.updatedBy,
      createdAt: app.createdAt ?? now,
      updatedAt: now,
    },
  });
}

// deleteAppRecord removes an app's record, which takes it offline: the
// runner no longer finds it, and pear collects the unreferenced code blob.
export function deleteAppRecord(orgDid: DidString, space: SpaceRefString) {
  return deleteRecordIndexed(orgDid, orgDid, {
    space,
    repo: orgDid,
    collection: APP_COLLECTION,
    rkey: "self",
  });
}
