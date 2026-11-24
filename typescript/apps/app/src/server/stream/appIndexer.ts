import type { LexMap } from "@atproto/lex";
import { SpaceRef } from "@atproto/syntax";
import { LexIndexer, type SpaceEvent } from "@habitat-network/jetstream/effect";
import { Effect } from "effect";
import { getDb } from "@/db";
import { APP_SPACE_TYPE } from "../apps/apps.server";
import { appsDomain } from "../apps/sink";
import { getSpaceBlob } from "../blobs";
import { docsDomain } from "../docs/sink";
import { FILE_SPACE_TYPE } from "../files/files.server";
import { filesDomain } from "../files/sink";
import { MEMBERS_SPACE_TYPE, membersDomain } from "../members/sink";
import type { Domain } from "./domain";

// appIndexer is the one indexer the app runs over a group's stream: each
// domain's handlers, registered for the space types it lives in. Groups'
// members spaces feed the rosters, file spaces Drop, app spaces the Apps
// page, and every other space (docs and their comments) the doc mirror.
// The collections the stream is asked for are the ones registered here
// (indexer.collections()).
export function appIndexer(env: Env): LexIndexer {
  const db = getDb(env);
  const indexer = new LexIndexer();
  const dedicated = [FILE_SPACE_TYPE, MEMBERS_SPACE_TYPE, APP_SPACE_TYPE];
  register(indexer, (t) => t === FILE_SPACE_TYPE, filesDomain(db));
  register(indexer, (t) => t === MEMBERS_SPACE_TYPE, membersDomain(db));
  register(indexer, (t) => t === APP_SPACE_TYPE, appsDomain(db));
  register(
    indexer,
    (t) => !dedicated.includes(t),
    // A doc's crdt state is a blob, read back out of pear as the group.
    docsDomain(env, {
      getBlob: (space, did, cid) => getSpaceBlob(env, space, did, cid),
    }),
  );
  return indexer;
}

// register adds a domain's handlers to the indexer, but only for events in
// spaces whose type passes inType.
function register(
  indexer: LexIndexer,
  inType: (spaceType: string) => boolean,
  domain: Domain,
): void {
  const here = (event: SpaceEvent) => inType(spaceTypeOf(event.space));
  for (const { collection, handlers } of domain.collections) {
    indexer.commit<LexMap>(collection, {
      put: (event) => (here(event) && handlers.put?.(event)) || Effect.void,
      del: (event) => (here(event) && handlers.del?.(event)) || Effect.void,
    });
  }
  indexer.spaceDeleted((event) =>
    here(event) ? domain.spaceDeleted(event) : Effect.void,
  );
}

function spaceTypeOf(space: string): string {
  try {
    return SpaceRef.parse(space).spaceType;
  } catch {
    return "";
  }
}
