import type { DidString, NsidString, SpaceRefString } from "@atproto/syntax";
import { com } from "api";
import { Effect } from "effect";
import { AppEnv, Habitat } from "../effect/services";
import type { FollowState } from "./orgStream";

export type { FollowState };

// The request side of the org streams (see orgStream.ts). Each org's stream
// lives in its own OrgStream Durable Object; these helpers reach it.

function stub(env: Env, org: string) {
  return env.ORG_STREAM.get(env.ORG_STREAM.idFromName(org));
}

// followOrg is called on a request made against org: it makes sure the
// org's stream is running (catching D1 up first if it wasn't). Calls share
// the org's single Durable Object, so there is one connection per org.
export async function followOrg(env: Env, org: string): Promise<FollowState> {
  return stub(env, org).follow(org);
}

// awaitIndexed waits for a write to reach D1 through org's stream: uri at
// cid, or deleted when cid is null. False when it wasn't indexed in time
// (the stream is behind or reconnecting): D1 catches up shortly.
export const awaitIndexed = Effect.fn("stream.awaitIndexed")(function* (
  org: string,
  uri: string,
  cid: string | null,
) {
  const env = yield* AppEnv;
  return yield* Effect.tryPromise(() =>
    stub(env, org).awaitRecord(org, uri, cid),
  ).pipe(
    Effect.catch((err) =>
      Effect.logWarning("awaiting indexing failed", err).pipe(Effect.as(false)),
    ),
  );
});

// What identifies the record a write is made to.
export interface RecordTarget {
  space: SpaceRefString;
  repo: DidString;
  collection: NsidString;
  rkey: string;
}

// putRecordIndexed writes a record as writer (their own session, through
// the typed Habitat service) and returns once org's stream has indexed it,
// so D1 reflects it with no optimistic update. `indexed` is false when the
// write was made but not seen in time.
export const putRecordIndexed = Effect.fn("stream.putRecord")(function* (
  org: string,
  writer: string,
  input: RecordTarget & { record: Record<string, unknown> },
) {
  const habitat = yield* Habitat;
  const { uri, cid } = yield* habitat.xrpc(
    writer,
    com.atproto.space.putRecord.main,
    { body: input as never },
  );
  return { uri, cid, indexed: yield* awaitIndexed(org, uri, cid) };
});

// deleteRecordIndexed deletes a record as writer and returns once org's
// stream has indexed the delete. Deleting a record that isn't there is not
// an error, and produces no event to wait for.
export const deleteRecordIndexed = Effect.fn("stream.deleteRecord")(function* (
  org: string,
  writer: string,
  target: RecordTarget,
) {
  const habitat = yield* Habitat;
  const found = yield* habitat
    .xrpc(writer, com.atproto.space.getRecord.main, { params: target })
    .pipe(
      Effect.map(({ uri }) => uri),
      Effect.catchTag("HabitatError", (e) =>
        e.status === 404 || e.error === "RecordNotFound"
          ? Effect.succeed(undefined)
          : Effect.fail(e),
      ),
    );
  yield* habitat.xrpc(writer, com.atproto.space.deleteRecord.main, {
    body: target,
  });
  if (!found) return { indexed: true };
  return { indexed: yield* awaitIndexed(org, found, null) };
});
