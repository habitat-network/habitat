import type { LexMap } from "@atproto/lex";
import type { SpaceEvent } from "@habitat-network/jetstream";
import { Effect } from "effect";
import { env } from "cloudflare:workers";
import { appIndexer } from "@/server/stream/appIndexer";

export const RECORD_CID =
  "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm";

const TIME = "2026-11-20T00:00:00.000000Z";
let seq = 0;

// putEvent is the stream event for a record write.
export function putEvent(
  space: string,
  did: string,
  collection: string,
  rkey: string,
  record: LexMap | Record<string, unknown>,
): SpaceEvent {
  return {
    kind: "put",
    operation: "create",
    seq: ++seq,
    space,
    did,
    uri: `${space}/${did}/${collection}/${rkey}`,
    collection,
    rkey,
    rev: "3l3qo2vutsw2b",
    time: TIME,
    cid: RECORD_CID,
    record: record as Record<string, unknown>,
  };
}

// delEvent is the stream event for a record delete.
export function delEvent(
  space: string,
  did: string,
  collection: string,
  rkey: string,
): SpaceEvent {
  return {
    kind: "del",
    operation: "delete",
    seq: ++seq,
    space,
    did,
    uri: `${space}/${did}/${collection}/${rkey}`,
    collection,
    rkey,
    rev: "3l3qo2vutsw2b",
    time: TIME,
  };
}

export function spaceDeletedEvent(space: string): SpaceEvent {
  return { kind: "spaceDeleted", seq: ++seq, space, time: TIME };
}

// indexEvents runs events through the app's indexer, in order, as the
// stream does.
export async function indexEvents(...events: SpaceEvent[]): Promise<void> {
  const indexer = appIndexer(env);
  for (const event of events) await Effect.runPromise(indexer.handle(event));
}
