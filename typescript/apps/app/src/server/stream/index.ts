import type { PutRecordInput, RecordTarget } from "@habitat-network/jetstream";
import { connectedOrgDids, getDb } from "@/db";
import type { WriteOutcome } from "./orgStream";

// The request side of the group streams (see orgStream.ts). Each group's
// stream lives in its own OrgStream Durable Object; these helpers reach it.

function stub(env: Env, org: string) {
  return env.ORG_STREAM.get(env.ORG_STREAM.idFromName(org));
}

// followOrg starts following org's spaces, if nothing is yet. Called when a
// group is connected, and on every cron run for every connected group.
export async function followOrg(env: Env, org: string): Promise<void> {
  await stub(env, org).ensure(org);
}

// followConnectedOrgs is the cron trigger's job: make sure every connected
// group's stream is running (a Durable Object that was evicted restarts
// from its saved cursor).
export async function followConnectedOrgs(env: Env): Promise<void> {
  const orgs = await connectedOrgDids(getDb(env));
  const results = await Promise.allSettled(
    orgs.map((org) => followOrg(env, org)),
  );
  results.forEach((r, i) => {
    if (r.status === "rejected") {
      console.warn("[stream] following group failed", orgs[i], r.reason);
    }
  });
}

/** The write was refused: nothing was written. */
export class StreamWriteError extends Error {
  constructor(
    message: string,
    readonly status?: number,
    readonly error?: string,
  ) {
    super(message);
  }
}

export interface IndexedWrite {
  uri: string;
  cid: string;
  // False when the write succeeded but its event wasn't indexed in time
  // (the stream is behind or reconnecting): D1 catches up shortly.
  indexed: boolean;
}

function settle(outcome: WriteOutcome): IndexedWrite {
  if (outcome.ok) return { uri: outcome.uri, cid: outcome.cid, indexed: true };
  if (outcome.tag === "WriteNotObserved") {
    console.warn("[stream] write not indexed in time:", outcome.message);
    return { uri: "", cid: "", indexed: false };
  }
  throw new StreamWriteError(outcome.message, outcome.status, outcome.error);
}

// putRecordIndexed writes a record as writer through org's stream, and
// returns once D1 reflects it: no optimistic update needed.
export async function putRecordIndexed(
  env: Env,
  org: string,
  writer: string,
  input: PutRecordInput,
): Promise<IndexedWrite> {
  return settle(await stub(env, org).putRecord(writer, input));
}

// deleteRecordIndexed deletes a record as writer through org's stream, and
// returns once D1 reflects it.
export async function deleteRecordIndexed(
  env: Env,
  org: string,
  writer: string,
  target: RecordTarget,
): Promise<IndexedWrite> {
  return settle(await stub(env, org).deleteRecord(writer, target));
}

// awaitIndexed waits for a write pear made itself (a procedure such as
// ejectMember) to reach D1: uri at cid, or deleted when cid is null.
export async function awaitIndexed(
  env: Env,
  org: string,
  uri: string,
  cid: string | null,
): Promise<IndexedWrite> {
  return settle(await stub(env, org).awaitRecord(uri, cid));
}
