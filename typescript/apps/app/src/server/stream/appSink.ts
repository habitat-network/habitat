import { SpaceRef } from "@atproto/syntax";
import { getDb } from "@/db";
import { APP_SPACE_TYPE } from "../apps/apps.server";
import { AppRecordSink } from "../apps/sink";
import { DocSink } from "../docs/sink";
import { FILE_SPACE_TYPE } from "../files/files.server";
import { FileSink } from "../files/sink";
import { MEMBERS_SPACE_TYPE, MembershipSink } from "../members/sink";
import { getSpaceBlob } from "../blobs";
import type { RepoBatch, SyncSink } from "./batch";

// AppSink sends each batch to the sink for its space's type: file spaces to
// FileSink, app spaces to the AppRecordSink, groups' members spaces to the
// MembershipSink, everything else (doc and comments spaces) to the DocSink.
export class AppSink implements SyncSink {
  constructor(
    private docs: DocSink,
    private files: FileSink,
    private members: MembershipSink,
    private apps: AppRecordSink,
  ) {}

  // for builds the sink the group streams index with. A doc's crdt state is
  // a blob; it's read back out of pear as the group (see ../blobs).
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

  apply(batch: RepoBatch, signal: AbortSignal): Promise<void> {
    const type = SpaceRef.parse(batch.space).spaceType;
    if (type === FILE_SPACE_TYPE) return this.files.apply(batch, signal);
    if (type === MEMBERS_SPACE_TYPE) return this.members.apply(batch, signal);
    if (type === APP_SPACE_TYPE) return this.apps.apply(batch, signal);
    return this.docs.apply(batch, signal);
  }
}
