import { waitUntil } from "cloudflare:workers";
import { SpaceRef, type DidString, type SpaceRefString } from "@atproto/syntax";
import { HabitatIdentityResolver } from "@habitat-network/habitat";
import { Duration } from "effect";
import {
  createSpaceSyncer,
  type CreateSpaceSyncerOptions,
  type PromiseRepoBatch,
  type PromiseSpaceSyncer,
  type PromiseSyncSink,
  type SpaceState,
} from "internal/spaceSync";
import { DrizzleSyncStore } from "internal/spaceSync/drizzle";
import { docAccessSubjects, docByUri, getDb } from "@/db";
import { docSpaceUriForComments } from "./docs/comments.server";
import { DocSink } from "./docs/sink";
import { FILE_SPACE_TYPE } from "./files/files.server";
import { FileSink } from "./files/sink";
import { HabitatClient } from "./habitatClient";

// The app keeps its D1 mirror (docs, comments, sharing, files) and its doc
// rooms in step with the group spaces it uses through internal/spaceSync,
// as Chalk and Drop each did. There's no long-lived process: every
// SpaceSyncer here lives for one request (or one cron run) and is disposed
// when it's done. Everything the syncer needs to resume lives in D1
// (DrizzleSyncStore), so a fresh syncer picks up where the last left off.
//
// - Requests (creating a doc, uploading a file, connecting a group, inbound
//   notifications) build a syncer that never claims due spaces itself
//   (schedulerPageSize 0): it only runs the passes the request asked for,
//   finishing them in waitUntil after the response has gone out.
// - The cron trigger (scheduled() in entry.ts) builds one that does: its
//   scheduler claims every due space from D1, and the run waits for those
//   passes before disposing.

// The cap on retry backoff. A pass cut off by the isolate going away leaves
// its space leased until this expires, so it's kept well under the
// library's one-hour default.
const BACKOFF_CAP = Duration.minutes(10);

// How long an upload or delete waits for its space's sync pass, so the file
// list already reflects it when the request returns. Past this the request
// still succeeds; the pass carries on in waitUntil and the list's polling
// picks the change up.
const WRITE_SYNC_WAIT_MS = 10_000;

// serviceDid is the app's own service identifier: the `aud` inbound space
// notifications are addressed to, and what registerNotify subscribes.
// Served from /.well-known/did.json.
export function serviceDid(env: Env): string {
  return `did:web:${new URL(env.APP_BASE_URL).host}`;
}

// delegationCandidates lists the identities whose sessions might mint a
// delegation token for space, best first: the space authority (the group,
// whose session the app holds once an admin has connected it), then a doc's
// recorded owner, then everyone with a direct grant on the doc. A doc
// shared from a group nobody has connected still syncs as long as one of
// its grantees has signed in.
async function delegationCandidates(
  env: Env,
  space: SpaceRefString,
): Promise<string[]> {
  const db = getDb(env);
  const docSpace = docSpaceUriForComments(space) ?? space;
  const [doc, grantees] = await Promise.all([
    docByUri(db, docSpace),
    docAccessSubjects(db, docSpace),
  ]);
  return [
    ...new Set([
      SpaceRef.parse(space).spaceDid,
      ...(doc ? [doc.ownerDid] : []),
      ...grantees,
    ]),
  ];
}

// issueDelegation mints a delegation token for space from the first
// candidate (see delegationCandidates) whose session can.
export async function issueDelegation(
  env: Env,
  space: SpaceRefString,
): Promise<string> {
  const errors: unknown[] = [];
  for (const did of await delegationCandidates(env, space)) {
    try {
      const { token } = await new HabitatClient(env, did).call<{
        token: string;
      }>("com.atproto.space.getDelegationToken", "GET", { space });
      return token;
    } catch (err) {
      errors.push(err);
    }
  }
  throw new AggregateError(errors, `no session can access ${space}`);
}

// AppSink sends each batch to the sink for its space's type: file spaces to
// Drop's FileSink, everything else (doc and comments spaces) to Chalk's
// DocSink.
class AppSink implements PromiseSyncSink {
  constructor(
    private docs: DocSink,
    private files: FileSink,
  ) {}

  apply(batch: PromiseRepoBatch, signal: AbortSignal): Promise<void> {
    const type = SpaceRef.parse(batch.space).spaceType;
    return type === FILE_SPACE_TYPE
      ? this.files.apply(batch, signal)
      : this.docs.apply(batch, signal);
  }
}

function newSyncer(
  env: Env,
  options: Partial<CreateSpaceSyncerOptions> = {},
): PromiseSpaceSyncer {
  // The doc sink reads crdt blobs back out through the syncer it's applying
  // batches for, so it gets a handle to it once it exists.
  let syncer: PromiseSpaceSyncer | undefined;
  const db = getDb(env);
  const sink = new AppSink(
    new DocSink(env, {
      getBlob: (space, did, cid) => {
        if (!syncer) throw new Error("syncer not started");
        return syncer.getBlob(space, did, cid);
      },
    }),
    new FileSink({ db }),
  );
  syncer = createSpaceSyncer({
    serviceDid: serviceDid(env),
    // Resolve space authorities (group DIDs) through pear too.
    identityResolver: new HabitatIdentityResolver(env.APP_HABITAT_URL),
    backoffCap: BACKOFF_CAP,
    store: new DrizzleSyncStore(db),
    sink,
    delegation: { issue: (space) => issueDelegation(env, space) },
    ...options,
  });
  return syncer;
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

// watchSpaces starts syncing spaces right away (and from then on via the
// cron run and inbound notifications). Needed right after creating a space:
// nothing else tells the app it exists. Returns once the spaces are
// recorded; their first passes finish in the background.
export async function watchSpaces(
  env: Env,
  spaces: readonly string[],
): Promise<void> {
  if (spaces.length === 0) return;
  const syncer = requestSyncer(env);
  try {
    await Promise.all(spaces.map((space) => syncer.watch(space)));
  } finally {
    finishInBackground(syncer, spaces);
  }
}

// syncNow watches space and waits (briefly) for its pass, so a write the
// request just made is mirrored by the time it returns. It reports the
// first pass's error, if it failed before ever completing.
export async function syncNow(
  env: Env,
  space: SpaceRefString,
): Promise<{ syncError?: string }> {
  const syncer = requestSyncer(env);
  try {
    await syncer.watch(space);
    await Promise.race([
      syncer.awaitIdle(space),
      new Promise((resolve) => setTimeout(resolve, WRITE_SYNC_WAIT_MS)),
    ]);
  } finally {
    finishInBackground(syncer, [space]);
  }
  const state = await new DrizzleSyncStore(getDb(env)).getSpace(space);
  if (state?.lastError && state.lastFullPassAt === undefined) {
    return { syncError: state.lastError };
  }
  return {};
}

// listSpacesOf lists the spaces of the given types did can list: for a
// group, `owner` is the group's DID, listing the spaces it owns; for a
// member it's omitted, listing the spaces they belong to.
async function listSpacesOf(
  env: Env,
  did: string,
  spaceTypes: readonly string[],
  owner?: string,
): Promise<string[]> {
  const client = new HabitatClient(env, did);
  const lists = await Promise.all(
    spaceTypes.map((spaceType) =>
      client.call<{ spaces: { uri: string }[] }>(
        "network.habitat.space.listSpaces",
        "GET",
        { spaceType, ...(owner ? { did: owner } : {}) },
      ),
    ),
  );
  return lists.flatMap(({ spaces }) => spaces.map((s) => s.uri));
}

// watchSpacesOf starts syncing every doc, comments and file space did can
// list: called once a member signs in, and once a group is connected, so
// docs and files created elsewhere are mirrored without waiting for anyone
// to open them.
export async function watchSpacesOf(
  env: Env,
  did: string,
  spaceTypes: readonly string[],
  owner?: string,
): Promise<void> {
  await watchSpaces(env, await listSpacesOf(env, did, spaceTypes, owner));
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

// getSpaceBlob reads a blob from a space's host, signed with the space
// credential the syncer mints. The app keeps no copy of uploaded files.
export async function getSpaceBlob(
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
