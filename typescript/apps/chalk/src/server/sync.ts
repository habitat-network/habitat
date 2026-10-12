import { waitUntil } from "cloudflare:workers";
import { SpaceRef, type SpaceRefString } from "@atproto/syntax";
import { HabitatIdentityResolver } from "@habitat-network/habitat";
import { Duration } from "effect";
import {
  createSpaceSyncer,
  type CreateSpaceSyncerOptions,
  type PromiseSpaceSyncer,
  type SpaceState,
} from "internal/spaceSync";
import { docAccessSubjects, docByUri, getDb } from "../db";
import { DrizzleSyncStore } from "../db/syncStore";
import { COMMENTS_SPACE_TYPE, docSpaceUriForComments } from "./comments.server";
import { DOCS_SPACE_TYPE } from "./functions.server";
import { HabitatClient } from "./habitatClient";
import { DocSink } from "./sink";

// chalk keeps its D1 mirror and DocRooms in step with doc spaces through
// internal/spaceSync, the same way Drop does (see
// typescript/apps/drop/src/server/sync.ts, whose shape this follows). There
// is no long-lived process: every SpaceSyncer here lives for one request (or
// one cron run) and is disposed when it's done. Everything the syncer needs
// to resume lives in D1 (DrizzleSyncStore), so a fresh syncer picks up
// exactly where the last one left off.
//
// - Requests (creating a doc or its comments space, signing in, connecting
//   an org, inbound notifications) build a syncer that never claims due
//   spaces itself (schedulerPageSize 0) — it only runs the passes the
//   request asked for, finishing them in waitUntil after the response has
//   gone out.
// - The cron trigger (scheduled() in entry.ts) builds one that does: its
//   scheduler claims every due space from D1, and the run waits for those
//   passes before disposing.

// The cap on retry backoff. A pass cut off by the isolate going away leaves
// its space leased until this expires, so it's kept well under the
// library's one-hour default, as in Drop.
const BACKOFF_CAP = Duration.minutes(10);

// serviceDid is chalk's own service identifier: the `aud` inbound space
// notifications are addressed to, and what registerNotify subscribes.
// Served from /.well-known/did.json (src/routes/[.well-known].did[.]json.ts).
export function serviceDid(env: Env): string {
  return `did:web:${new URL(env.CHALK_BASE_URL).host}`;
}

// delegationCandidates lists the identities whose sessions might mint a
// delegation token for space, best first: the space authority (the doc's
// owner, or the org for an org doc — chalk holds the org's session once a
// member has connected it), then the doc's recorded owner, then everyone
// with a direct grant on the doc. Any member with access can mint one, so
// a doc shared from an org nobody has connected still syncs as long as one
// of its grantees has signed in to chalk.
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

function newSyncer(
  env: Env,
  options: Partial<CreateSpaceSyncerOptions> = {},
): PromiseSpaceSyncer {
  // The sink reads crdt blobs back out through the syncer it's applying
  // batches for, so it gets a handle to it once it exists.
  let syncer: PromiseSpaceSyncer | undefined;
  const sink = new DocSink(env, {
    getBlob: (space, did, cid) => {
      if (!syncer) throw new Error("syncer not started");
      return syncer.getBlob(space, did, cid);
    },
  });
  syncer = createSpaceSyncer({
    serviceDid: serviceDid(env),
    // Resolve space authorities (org DIDs) through pear too, not just OAuth.
    identityResolver: new HabitatIdentityResolver(env.CHALK_HABITAT_URL),
    backoffCap: BACKOFF_CAP,
    store: new DrizzleSyncStore(getDb(env)),
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
// nothing else tells chalk it exists. Returns once the spaces are recorded;
// their first passes finish in the background.
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

// watchDocSpacesOf starts syncing every doc space and comments space did
// can list — called once a member signs in, and once an org is connected,
// so docs created elsewhere (another chalk deployment, or before this one
// held the session) are mirrored without waiting for anyone to open them.
// For an org, `owner` is the org's DID, listing the spaces it owns; for a
// member it's omitted, listing the spaces they belong to.
export async function watchDocSpacesOf(
  env: Env,
  did: string,
  owner?: string,
): Promise<void> {
  const client = new HabitatClient(env, did);
  const lists = await Promise.all(
    [DOCS_SPACE_TYPE, COMMENTS_SPACE_TYPE].map((spaceType) =>
      client.call<{ spaces: { uri: string }[] }>(
        "network.habitat.space.listSpaces",
        "GET",
        { spaceType, ...(owner ? { did: owner } : {}) },
      ),
    ),
  );
  await watchSpaces(
    env,
    lists.flatMap(({ spaces }) => spaces.map((s) => s.uri)),
  );
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

// How long to wait for the scheduler's first tick to claim the spaces that
// were due when the cron run started. The tick runs as soon as the syncer
// starts, so this only bounds a slow D1.
const CLAIM_WAIT_MS = 10_000;
const CLAIM_POLL_MS = 100;

// runDueSpaces is the cron trigger's job: start a syncer whose scheduler
// claims every space due in D1, wait for those passes, and dispose it.
// Spaces that come due while it runs are left for the next run. Same as
// Drop's runDueSpaces.
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
