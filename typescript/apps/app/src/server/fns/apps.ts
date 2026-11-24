import { createServerFn } from "@tanstack/react-start";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import { Effect, Schema } from "effect";
import { appByUri, appsForOrg, type AppSummary } from "@/db";
import {
  DEFAULT_COMPATIBILITY_DATE,
  InvalidBundle,
  runUrl,
  slugify,
  validateBundle,
} from "@/lib/apps";
import {
  createAppSpace,
  deleteAppRecord,
  putAppRecord,
  uploadBundle,
} from "@/server/apps/apps.server";
import { BadRequest, NotFound } from "@/server/effect/errors";
import { requireOrg } from "@/server/effect/member";
import { runEffect } from "@/server/effect/run";
import { AppEnv, attempt, Database, Habitat } from "@/server/effect/services";
import { getProfiles, requireConnected } from "@/server/groups";

// Apps: code a member uploads, which the app runs in a sandboxed Dynamic
// Worker at its own URL (src/server/apps/runner.ts). Each app is its own
// group space (type network.habitat.apps, readable by every member) holding
// one network.habitat.apps.app record, written with the group's own
// session, which names the app and references its code bundle as a blob.
// The list is the stream's output, never the app's own writes. Any member
// may create, redeploy or delete an app, as with files.

export interface AppView extends AppSummary {
  url: string;
  updatedByName: string | null;
}

const OrgInput = Schema.Struct({ org: Schema.String });

// listApps returns the group's apps, most recently deployed first.
export const listApps = createServerFn({ method: "GET" })
  .validator(Schema.decodeUnknownSync(OrgInput))
  .handler(({ data }) =>
    runEffect(
      "apps.list",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const db = yield* Database;
        const env = yield* AppEnv;
        const rows = yield* Effect.promise(() => appsForOrg(db, org));
        const deployers = [
          ...new Set(rows.flatMap((r) => (r.updatedBy ? [r.updatedBy] : []))),
        ];
        const profiles = yield* getProfiles(did, org, deployers);
        return rows.map((r): AppView => {
          const p = r.updatedBy ? profiles.get(r.updatedBy) : undefined;
          return {
            ...r,
            url: runUrl(env.APP_BASE_URL, env.APP_RUN_DOMAIN, r.host),
            updatedByName: p?.displayName || p?.handle || null,
          };
        });
      }),
    ),
  );

const DeployInput = Schema.Struct({
  org: Schema.String,
  // The app to deploy a new version of; a new app when omitted.
  uri: Schema.optional(Schema.String),
  name: Schema.String,
  description: Schema.optional(Schema.String),
  entrypoint: Schema.String,
  modules: Schema.Record(Schema.String, Schema.String),
});

// deployApp creates an app, or deploys a new version of one: the bundle is
// uploaded as a blob and the app's record rewritten to reference it. Returns
// once the group's stream has indexed the record, so the run URL serves the
// new version as soon as this returns (unless syncError says otherwise).
export const deployApp = createServerFn({ method: "POST" })
  .validator(Schema.decodeUnknownSync(DeployInput))
  .handler(({ data }) =>
    runEffect(
      "apps.deploy",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        yield* requireConnected(org);
        const name = data.name.trim();
        if (!name) return yield* new BadRequest({ message: "Name the app" });
        if (name.length > 200) {
          return yield* new BadRequest({ message: "That name is too long" });
        }
        const bundle = { modules: data.modules };
        yield* Effect.try({
          try: () => validateBundle(bundle, data.entrypoint),
          catch: (e) =>
            new BadRequest({
              message: e instanceof InvalidBundle ? e.message : String(e),
            }),
        });

        const db = yield* Database;
        const habitat = yield* Habitat;
        const session = yield* habitat.session(org);

        const existing = data.uri
          ? yield* Effect.promise(() => appByUri(db, data.uri as string))
          : undefined;
        // An app in another group looks the same as one that doesn't exist.
        if (data.uri && (!existing || existing.orgDid !== org)) {
          return yield* new NotFound({ message: "App not found" });
        }
        const space = existing
          ? (existing.space as SpaceRefString)
          : yield* attempt(org, "apps.createSpace", () =>
              createAppSpace(session, org),
            );
        const { blob } = yield* attempt(org, "apps.uploadBundle", () =>
          uploadBundle(session, bundle),
        );
        const description = data.description?.trim().slice(0, 2000);
        const { indexed } = yield* putAppRecord(org, space, {
          name,
          slug: existing?.slug ?? slugify(name),
          description: description || undefined,
          entrypoint: data.entrypoint,
          code: blob,
          version: (existing?.version ?? 0) + 1,
          compatibilityDate:
            existing?.compatibilityDate ?? DEFAULT_COMPATIBILITY_DATE,
          createdBy: existing?.createdBy ?? undefined,
          createdAt: existing
            ? new Date(existing.createdAt).toISOString()
            : undefined,
          updatedBy: did as DidString,
        });
        yield* Effect.logInfo("deployed app", {
          space,
          version: (existing?.version ?? 0) + 1,
          modules: Object.keys(data.modules).length,
          indexed,
        });
        const uri = `${space}/${org}/network.habitat.apps.app/self`;
        const syncError = indexed
          ? undefined
          : "Deployed, but it may take a moment to go live";
        return { uri, syncError };
      }),
    ),
  );

// deleteApp deletes an app's record, which takes it offline at once: the
// runner no longer finds it, and its code blob is collected by pear.
export const deleteApp = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({ org: Schema.String, uri: Schema.String }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "apps.delete",
      Effect.gen(function* () {
        const { org } = yield* requireOrg(data.org);
        yield* requireConnected(org);
        const db = yield* Database;
        const app = yield* Effect.promise(() => appByUri(db, data.uri));
        if (!app || app.orgDid !== org) {
          return yield* new NotFound({ message: "App not found" });
        }
        yield* deleteAppRecord(org as DidString, app.space as SpaceRefString);
        yield* Effect.logInfo("deleted app", { app: data.uri });
      }),
    ),
  );
