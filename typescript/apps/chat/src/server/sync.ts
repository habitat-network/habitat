import { waitUntil } from "cloudflare:workers";
import { type DidString, SpaceRef, type SpaceRefString } from "@atproto/syntax";
import { HabitatIdentityResolver } from "@habitat-network/habitat";
import { Duration } from "effect";
import {
  createSpaceSyncer,
  type CreateSpaceSyncerOptions,
  type PromiseSpaceSyncer,
  type SpaceState,
} from "internal/spaceSync";
import { connectedOrgIds, getDb } from "@/db";
import { DrizzleSyncStore } from "internal/spaceSync/drizzle";
import {
  createChannelSpace,
  getDelegationToken,
  listOrgChannelSpaces,
  membersSpace,
  putChannelRecord,
} from "./habitat";
import { createOAuthClient } from "./oauth";
import { chatRuntime } from "./runtime";

// Chat has no long-lived process: every SpaceSyncer here lives for one
// request (or one cron run) and is disposed when it's done. Everything the
// syncer needs to resume lives in D1 (DrizzleSyncStore), so a fresh syncer
// picks up exactly where the last one left off.
//
// - Requests (channel creation, connects, inbound notifications) build a
//   syncer that never claims due spaces itself (schedulerPageSize 0) — it
//   only runs the passes the request asked for, finishing them in
//   waitUntil after the response has gone out.
// - The cron trigger (scheduled() in entry.ts) builds one that does: its
//   scheduler claims every due space from D1, and the run waits for those
//   passes before disposing.

// How long creating a channel waits for the new channel's space to finish
// its first sync pass, so the channel is in the synced list by the time the
// request returns. Past this the request still succeeds; the pass carries
// on in waitUntil and the browser's polling picks the channel up.
const CREATE_SYNC_WAIT_MS = 10_000;

// The lease a claimed (or newly watched) space holds while its pass runs,
// and the cap on retry backoff. A pass cut off by the isolate going away
// leaves its space leased until this expires, so it's kept well under the
// library's one-hour default.
const BACKOFF_CAP = Duration.minutes(10);

// serviceDid is Chat's own service identifier: the `aud` inbound space
// notifications are addressed to, and what registerNotify subscribes.
// Served from /.well-known/did.json (src/routes/[.well-known].did[.]json.ts).
export function serviceDid(env: Env): string {
  return `did:web:${new URL(env.CHAT_BASE_URL).host}`;
}

function newSyncer(
  env: Env,
  options: Partial<CreateSpaceSyncerOptions> = {},
): PromiseSpaceSyncer {
  const db = getDb(env);
  const oauth = createOAuthClient(env);
  const runtime = chatRuntime(env);
  return createSpaceSyncer({
    serviceDid: serviceDid(env),
    // Resolve space authorities (org DIDs) through pear too, not just OAuth.
    identityResolver: new HabitatIdentityResolver(env.CHAT_HABITAT_URL),
    backoffCap: BACKOFF_CAP,
    store: new DrizzleSyncStore(db),
    // The Habitat app runtime's sink keeps the slice (records plus the ACL
    // tables) in step with every synced space; nothing here is specific to
    // Chat's record types.
    sink: {
      apply: async (batch, signal) => {
        await runtime.migrate();
        await runtime.sink.apply(batch, signal);
      },
    },
    // Every space Chat syncs (channels and the members space) belongs to
    // an org, and Chat holds that org's own session (that's what connecting
    // an org is), so the space authority is always the identity to mint
    // delegation tokens as.
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

// connectOrg starts syncing the org's members space (which the slice's
// permissions are derived from) and every channel the org already owns.
// Called once an org's session is in place.
export async function connectOrg(env: Env, orgDid: DidString): Promise<void> {
  const agent = await createOAuthClient(env).restore(orgDid);
  const spaces = [
    membersSpace(orgDid),
    ...(await listOrgChannelSpaces(agent, orgDid)),
  ];
  const syncer = requestSyncer(env);
  try {
    await Promise.all(spaces.map((space) => syncer.watch(space)));
  } finally {
    finishInBackground(syncer, spaces);
  }
}

export interface CreateChannelInput {
  orgDid: DidString;
  memberDid: DidString;
  name: string;
  topic?: string;
  // The opensocial roles that may read and write the channel.
  roles: string[];
}

// createChannel creates the channel's own org space, open to the given
// roles, and writes the record that names it. Both are written as the org.
// The channel list is only ever filled in by the syncer: this waits
// (briefly) for the new space's first pass rather than writing rows itself.
export async function createChannel(
  env: Env,
  input: CreateChannelInput,
): Promise<{ space: SpaceRefString; syncError?: string }> {
  const agent = await createOAuthClient(env).restore(input.orgDid);
  const space = await createChannelSpace(agent, input.orgDid, input.roles);
  await putChannelRecord(agent, input.orgDid, space, {
    name: input.name,
    topic: input.topic,
    createdBy: input.memberDid,
  });
  const syncer = requestSyncer(env);
  try {
    await syncer.watch(space);
    await Promise.race([
      syncer.awaitIdle(space),
      new Promise((resolve) => setTimeout(resolve, CREATE_SYNC_WAIT_MS)),
    ]);
  } finally {
    finishInBackground(syncer, [space]);
  }
  const state = await new DrizzleSyncStore(getDb(env)).getSpace(space);
  if (state?.lastError && state.lastFullPassAt === undefined) {
    console.error("[chat] first sync of", space, "failed:", state.lastError);
    return { space, syncError: state.lastError };
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

// discoverChannels re-lists every connected org's channels and starts
// syncing the ones this deployment isn't watching yet (channels created by
// another deployment or app). Only new spaces are watched: watching always
// queues a pass, and the ones already known are kept current by
// notifications and their own schedule. A failure for one org is logged
// and doesn't stop the others.
export async function discoverChannels(env: Env): Promise<void> {
  const db = getDb(env);
  const store = new DrizzleSyncStore(db);
  const oauth = createOAuthClient(env);
  for (const orgDid of await connectedOrgIds(db)) {
    try {
      const agent = await oauth.restore(orgDid);
      const listed = [
        membersSpace(orgDid as DidString),
        ...(await listOrgChannelSpaces(agent, orgDid as DidString)),
      ];
      const fresh: SpaceRefString[] = [];
      for (const space of listed) {
        if ((await store.getSpace(space)) === undefined) fresh.push(space);
      }
      if (fresh.length === 0) continue;
      const syncer = requestSyncer(env);
      try {
        await Promise.all(fresh.map((space) => syncer.watch(space)));
        await Promise.allSettled(fresh.map((space) => syncer.awaitIdle(space)));
      } finally {
        await syncer.dispose();
      }
    } catch (err) {
      console.error("[chat] discovering channels of", orgDid, err);
    }
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
