import type { DidString } from "@atproto/syntax";
import { appByHost, type AppRow, getDb } from "@/db";
import { appHostOf, runUrl } from "@/lib/apps";
import { getSpaceBlob } from "../blobs";
import { decodeBundle } from "./apps.server";

// The runner serves Apps: each request to <label>.<APP_RUN_DOMAIN> is
// answered by that app's code, in a Dynamic Worker the LOADER binding
// starts from the code bundle stored in pear.
//
// Sandboxing: each app is its own isolate, cached by the record it was
// deployed with, so a new deploy starts a fresh one. It has no network
// (globalOutbound: null makes fetch() and connect() throw), no bindings to
// the app's D1, Durable Objects or sessions, and a CPU limit per request.
// Its env holds only a description of itself (HABITAT). Each app runs on
// its own subdomain, so it shares no origin, storage or cookies with the
// Habitat app or with other apps.

// The CPU time an app's request may use before it is stopped.
export const APP_CPU_MS = 1_000;

// The isolate's own name: the app record at the version it was deployed
// with. The loader keeps an isolate warm per name, and the name must
// always mean the same code.
export function workerName(app: Pick<AppRow, "uri" | "cid">): string {
  return `${app.uri}@${app.cid}`;
}

type ModuleSource = WorkerLoaderWorkerCode["modules"][string];

// moduleOf gives a bundle file the module type its extension names.
export function moduleOf(path: string, source: string): ModuleSource {
  const lower = path.toLowerCase();
  if (lower.endsWith(".js") || lower.endsWith(".mjs")) return { js: source };
  if (lower.endsWith(".cjs")) return { cjs: source };
  if (lower.endsWith(".json")) return { json: JSON.parse(source) };
  return { text: source };
}

export interface RunnerDeps {
  // getBlob reads the app's code bundle out of its space.
  getBlob: (space: string, repo: DidString, cid: string) => Promise<Uint8Array>;
}

// workerCode builds what the loader starts an app's isolate with.
export async function workerCode(
  env: Env,
  app: AppRow,
  deps: RunnerDeps,
): Promise<WorkerLoaderWorkerCode> {
  const bytes = await deps.getBlob(
    app.space,
    app.repo as DidString,
    app.codeCid,
  );
  const bundle = decodeBundle(bytes);
  if (!(app.entrypoint in bundle.modules)) {
    throw new Error(`entrypoint ${app.entrypoint} isn't in the bundle`);
  }
  const modules: WorkerLoaderWorkerCode["modules"] = {};
  for (const [path, source] of Object.entries(bundle.modules)) {
    modules[path] = moduleOf(path, source);
  }
  return {
    compatibilityDate: app.compatibilityDate,
    mainModule: app.entrypoint,
    modules,
    env: {
      HABITAT: {
        app: app.uri,
        org: app.orgDid,
        name: app.name,
        version: app.version,
        url: runUrl(env.APP_BASE_URL, env.APP_RUN_DOMAIN, app.host),
      },
    },
    globalOutbound: null,
    limits: { cpuMs: APP_CPU_MS },
  };
}

function page(status: number, title: string, detail: string): Response {
  const body = `<!doctype html><meta charset="utf-8"><title>${title}</title><body style="font-family:system-ui;max-width:36rem;margin:4rem auto;padding:0 1rem"><h1 style="font-size:1.25rem">${title}</h1><p style="color:#555">${detail}</p></body>`;
  return new Response(body, {
    status,
    headers: {
      "content-type": "text/html; charset=utf-8",
      "cache-control": "no-store",
    },
  });
}

// runApp answers a request to the run domain.
export async function runApp(
  request: Request,
  env: Env,
  deps?: RunnerDeps,
): Promise<Response> {
  const label = appHostOf(new URL(request.url), env.APP_RUN_DOMAIN);
  if (!label) {
    return page(404, "No app here", "This address doesn't name an app.");
  }
  const app = await appByHost(getDb(env), label);
  if (!app) {
    return page(
      404,
      "App not found",
      "There's no app at this address. It may have been deleted.",
    );
  }
  // The bundle is read as the group itself: an app's record and code are
  // always in the group's own repo (the sink skips any other writer).
  const resolved: RunnerDeps = deps ?? {
    getBlob: (space, repo, cid) => getSpaceBlob(env, space, repo, cid),
  };
  const worker = env.LOADER.get(workerName(app), () =>
    workerCode(env, app, resolved),
  );
  try {
    return await worker.getEntrypoint().fetch(request);
  } catch (err) {
    console.warn("[apps] app failed", app.uri, err);
    return page(
      500,
      "This app failed",
      "The app's code threw an error or ran out of time handling this request.",
    );
  }
}
