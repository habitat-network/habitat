import { SpaceRef, type DidString, type SpaceRefString } from "@atproto/syntax";
import { HabitatIdentityResolver } from "@habitat-network/habitat";
import { Duration } from "effect";
import { trimTrailingSlashes } from "internal/oauthClient";
import {
  createSpaceSyncer,
  type CreateSpaceSyncerOptions,
  type PromiseRepoBatch,
  type PromiseSpaceSyncer,
  type PromiseSyncSink,
} from "internal/spaceSync";
import { DrizzleSyncStore } from "internal/spaceSync/drizzle";
import { docAccessSubjects, docByUri, getDb, memberDidsOf } from "@/db";
import { APP_SPACE_TYPE } from "./apps/apps.server";
import { AppRecordSink } from "./apps/sink";
import { docSpaceUriForComments } from "./docs/comments.server";
import { DocSink } from "./docs/sink";
import { FILE_SPACE_TYPE } from "./files/files.server";
import { FileSink } from "./files/sink";
import { HabitatClient } from "./habitatClient";
import { MEMBERS_SPACE_TYPE, MembershipSink } from "./members/sink";

// The app indexes its groups' spaces through pear's subscribe stream (see
// src/server/stream). What's left here is internal/spaceSync's credential
// machinery, for reading blobs: a doc's crdt state and uploaded files are
// blobs, which may live on a member's own PDS, and are read with a space
// credential minted from a delegation token. No syncer here syncs anything.

// The cap on retry backoff.
const BACKOFF_CAP = Duration.minutes(10);

// serviceDid is the app's own did:web identifier, served from
// /.well-known/did.json.
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
  // A group's roster is readable by its members, so any of them whose
  // session the app holds can mint the token too. The member who
  // bootstrapped it is one (see refreshMemberships), which is what lets the
  // roster's first sync run before the group is connected.
  if (SpaceRef.parse(space).spaceType === MEMBERS_SPACE_TYPE) {
    const org = SpaceRef.parse(space).spaceDid;
    // A handful is plenty: each is tried in turn until one can.
    const members = (await memberDidsOf(db, org)).slice(0, 10);
    return [...new Set([org, ...members])];
  }
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
// Drop's FileSink, app spaces to the AppRecordSink, groups' members spaces
// to the MembershipSink, everything else (doc and comments spaces) to
// Chalk's DocSink.
export class AppSink implements PromiseSyncSink {
  constructor(
    private docs: DocSink,
    private files: FileSink,
    private members: MembershipSink,
    private apps: AppRecordSink,
  ) {}

  // for builds the sink the group streams index with. Crdt blobs are read
  // out of their space with a space credential (getSpaceBlob).
  static for(env: Env): AppSink {
    const db = getDb(env);
    return new AppSink(
      new DocSink(env, {
        getBlob: (space, did, cid) => getSpaceBlob(env, space, did, cid),
      }),
      new FileSink({ db }),
      new MembershipSink({ db }),
      new AppRecordSink({ db }),
    );
  }

  apply(batch: PromiseRepoBatch, signal: AbortSignal): Promise<void> {
    const type = SpaceRef.parse(batch.space).spaceType;
    if (type === FILE_SPACE_TYPE) return this.files.apply(batch, signal);
    if (type === MEMBERS_SPACE_TYPE) return this.members.apply(batch, signal);
    if (type === APP_SPACE_TYPE) return this.apps.apply(batch, signal);
    return this.docs.apply(batch, signal);
  }
}

// A syncer used only for its credentials and blob reads: it watches no
// space, so its sink never runs.
const noSink: PromiseSyncSink = { apply: async () => {} };

function newSyncer(
  env: Env,
  options: Partial<CreateSpaceSyncerOptions> = {},
): PromiseSpaceSyncer {
  return createSpaceSyncer({
    notifyEndpoint: trimTrailingSlashes(env.APP_BASE_URL),
    notifySecret: process.env.APP_SESSION_SECRET,
    // Resolve space authorities (group DIDs) through pear too.
    identityResolver: new HabitatIdentityResolver(env.APP_HABITAT_URL),
    backoffCap: BACKOFF_CAP,
    store: new DrizzleSyncStore(getDb(env)),
    sink: noSink,
    delegation: { issue: (space) => issueDelegation(env, space) },
    ...options,
  });
}

// requestSyncer is a syncer for one request, with its scheduler off.
function requestSyncer(env: Env): PromiseSpaceSyncer {
  return newSyncer(env, { schedulerPageSize: 0 });
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
