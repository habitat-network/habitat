import { waitUntil } from "cloudflare:workers";
import { type DidString, SpaceRef, type SpaceRefString } from "@atproto/syntax";
import { Duration } from "effect";
import {
  createSpaceSyncer,
  type CreateSpaceSyncerOptions,
  type PromiseSpaceSyncer,
  type SpaceState,
} from "internal/spaceSync";
import { getDb } from "@/db";
import { DrizzleSyncStore } from "@/db/syncStore";
import {
  createFileSpace,
  getDelegationToken,
  listOrgFileSpaces,
  putFileRecord,
  uploadBlob,
} from "./habitat";
import { createOAuthClient } from "./oauth";
import { FileSink } from "./sink";

// Drop has no long-lived process: every SpaceSyncer here lives for one
// request (or one cron run) and is disposed when it's done. Everything the
// syncer needs to resume lives in D1 (DrizzleSyncStore), so a fresh syncer
// picks up exactly where the last one left off.
//
// - Requests (uploads, connects, inbound notifications, downloads) build a
//   syncer that never claims due spaces itself (schedulerPageSize 0) — it
//   only runs the passes the request asked for, finishing them in
//   waitUntil after the response has gone out.
// - The cron trigger (scheduled() in entry.ts) builds one that does: its
//   scheduler claims every due space from D1, and the run waits for those
//   passes before disposing.

// How long an upload waits for the new file's space to finish its first
// sync pass, so the file is in the synced list by the time the upload
// request returns. Past this the upload still succeeds; the pass carries on
// in waitUntil and the browser's polling picks the row up.
const UPLOAD_SYNC_WAIT_MS = 10_000;

// The lease a claimed (or newly watched) space holds while its pass runs,
// and the cap on retry backoff. A pass cut off by the isolate going away
// leaves its space leased until this expires, so it's kept well under the
// library's one-hour default: Drop's spaces each hold one small file, and
// their passes take seconds.
const BACKOFF_CAP = Duration.minutes(10);

export interface UploadInput {
  orgDid: DidString;
  memberDid: DidString;
  name: string;
  mimeType: string;
  bytes: Uint8Array;
}

// serviceDid is Drop's own service identifier: the `aud` inbound space
// notifications are addressed to, and what registerNotify subscribes.
// Served from /.well-known/did.json (src/routes/[.well-known].did[.]json.ts).
export function serviceDid(env: Env): string {
  return `did:web:${new URL(env.DROP_BASE_URL).host}`;
}

function newSyncer(
  env: Env,
  options: Partial<CreateSpaceSyncerOptions> = {},
): PromiseSpaceSyncer {
  const db = getDb(env);
  const oauth = createOAuthClient(env);
  return createSpaceSyncer({
    serviceDid: serviceDid(env),
    backoffCap: BACKOFF_CAP,
    store: new DrizzleSyncStore(db),
    sink: new FileSink({ db }),
    // Every Drop space belongs to an org, and Drop holds that org's own
    // session (that's what connecting an org is), so the space authority
    // is always the identity to mint delegation tokens as.
    delegation: {
      issue: async (space) => {
        const authority = SpaceRef.parse(space).spaceDid;
        return getDelegationToken(await oauth.restore(authority), space);
      },
    },
    ...options,
  });
}

// requestSyncer is a syncer for one request: it runs only the passes the
// request starts (watch, notifyWrite), never the scheduler's due spaces.
function requestSyncer(env: Env): PromiseSpaceSyncer {
  return newSyncer(env, { schedulerPageSize: 0 });
}

// finishInBackground keeps the isolate alive until the given spaces' passes
// are done, then disposes the syncer.
function finishInBackground(
  syncer: PromiseSpaceSyncer,
  spaces: readonly string[],
) {
  waitUntil(
    Promise.allSettled(spaces.map((space) => syncer.awaitIdle(space))).then(
      () => syncer.dispose(),
    ),
  );
}

// connectOrg starts syncing every file space the org already owns. Called
// once an org's session is in place.
export async function connectOrg(env: Env, orgDid: DidString): Promise<void> {
  const agent = await createOAuthClient(env).restore(orgDid);
  const spaces = await listOrgFileSpaces(agent, orgDid);
  const syncer = requestSyncer(env);
  try {
    await Promise.all(spaces.map((space) => syncer.watch(space)));
  } finally {
    finishInBackground(syncer, spaces);
  }
}

// upload creates the file's own org space (readable by every member),
// uploads its blob, and writes the record that names the file and keeps the
// blob from being garbage collected. The list itself is only ever filled in
// by the syncer: this waits (briefly) for the new space's first pass rather
// than writing the row itself.
export async function upload(
  env: Env,
  input: UploadInput,
): Promise<{ space: SpaceRefString }> {
  const agent = await createOAuthClient(env).restore(input.orgDid);
  const space = await createFileSpace(agent, input.orgDid);
  const { blob } = await uploadBlob(agent, input.bytes, input.mimeType);
  await putFileRecord(agent, input.orgDid, space, {
    name: input.name,
    blob,
    uploadedBy: input.memberDid,
  });
  const syncer = requestSyncer(env);
  try {
    await syncer.watch(space);
    await Promise.race([
      syncer.awaitIdle(space),
      new Promise((resolve) => setTimeout(resolve, UPLOAD_SYNC_WAIT_MS)),
    ]);
  } finally {
    finishInBackground(syncer, [space]);
  }
  return { space };
}

// Inbound space notifications from the Worker's XRPC route, with their raw
// body and Authorization header (the syncer verifies the service-auth JWT
// itself). Verification happens before these return; the pass a write
// triggers finishes in the background.
export async function notifyWrite(
  env: Env,
  body: unknown,
  authorization: string | undefined,
): Promise<void> {
  const syncer = requestSyncer(env);
  try {
    await syncer.notifyWrite(body, authorization);
  } finally {
    finishInBackground(syncer, notifiedSpace(body));
  }
}

export async function notifySpaceDeleted(
  env: Env,
  body: unknown,
  authorization: string | undefined,
): Promise<void> {
  const syncer = requestSyncer(env);
  try {
    await syncer.notifySpaceDeleted(body, authorization);
  } finally {
    finishInBackground(syncer, notifiedSpace(body));
  }
}

function notifiedSpace(body: unknown): string[] {
  const space = (body as { space?: unknown } | null)?.space;
  return typeof space === "string" ? [space] : [];
}

// getFileBlob reads a file's bytes from the org's space host, signed with
// the space credential the syncer mints from the org's session. Drop keeps
// no copy of its own.
export async function getFileBlob(
  env: Env,
  space: string,
  repo: DidString,
  cid: string,
): Promise<Uint8Array> {
  const syncer = requestSyncer(env);
  try {
    return await syncer.getBlob(space, repo, cid);
  } finally {
    await syncer.dispose();
  }
}

// How long to wait for the scheduler's first tick to claim the spaces that
// were due when the cron run started. The tick runs as soon as the syncer
// starts, so this only bounds a slow D1.
const CLAIM_WAIT_MS = 10_000;
const CLAIM_POLL_MS = 100;

// runDueSpaces is the cron trigger's job: start a syncer whose scheduler
// claims every space due in D1, wait for those passes, and dispose it.
// Spaces that come due while it runs are left for the next run.
export async function runDueSpaces(env: Env): Promise<void> {
  const store = new DrizzleSyncStore(getDb(env));
  const due = await store.dueSpaces(Date.now(), 500);
  if (due.length === 0) return;

  const syncer = newSyncer(env);
  try {
    // Constructing the syncer is lazy; awaitIdle on any space starts it,
    // and with it the scheduler's first tick.
    await syncer.awaitIdle(due[0].space);
    // A claim moves nextDueAt to the lease, and the scheduler queues the
    // pass right after with no I/O in between, so once a space's row has
    // moved its pass is visible to awaitIdle. (If another run claimed it,
    // awaitIdle here just returns.) Rows the scheduler skips, like an
    // unparseable space, never move and are dropped at the deadline.
    const deadline = Date.now() + CLAIM_WAIT_MS;
    let pending: readonly SpaceState[] = due;
    const claimed: string[] = [];
    while (pending.length > 0 && Date.now() < deadline) {
      const next: SpaceState[] = [];
      for (const state of pending) {
        const current = await store.getSpace(state.space);
        if (current?.nextDueAt === state.nextDueAt) next.push(state);
        else claimed.push(state.space);
      }
      pending = next;
      if (pending.length > 0) {
        await new Promise((resolve) => setTimeout(resolve, CLAIM_POLL_MS));
      }
    }
    await Promise.allSettled(claimed.map((space) => syncer.awaitIdle(space)));
  } finally {
    await syncer.dispose();
  }
}
