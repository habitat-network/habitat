import { DurableObject } from "cloudflare:workers";
import type { OAuthClient } from "@atproto/oauth-client";
import { type DidString, SpaceRef, type SpaceRefString } from "@atproto/syntax";
import { createSpaceSyncer, type PromiseSpaceSyncer } from "internal/spaceSync";
import { getDb } from "@/db";
import { DrizzleSyncStore } from "@/db/syncStore";
import { createOAuthClient } from "./oauth";
import { FileSink } from "./sink";
import {
  createFileSpace,
  getDelegationToken,
  listOrgFileSpaces,
  putFileRecord,
  uploadBlob,
} from "./habitat";

// How often the alarm wakes this object. The syncer's own scheduler fiber
// (every schedulerInterval) only runs while the object is in memory, and an
// idle Durable Object is evicted after a minute or two without events — the
// alarm both keeps it resident and, after an eviction, brings it back and
// restarts the syncer, which then picks up every due space from D1.
const ALARM_INTERVAL_MS = 30_000;

// How long an upload waits for the new file's space to finish its first
// sync pass, so the file is in the synced list by the time the upload
// request returns. Past this the upload still succeeds; the row lands when
// the pass does, and open browsers hear about it over the WebSocket.
const UPLOAD_SYNC_WAIT_MS = 10_000;

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

// SyncHub is the single home of Drop's space syncer and of every call made
// with an org's credentials. One instance (idFromName("hub")) serves the
// whole deployment:
//
// - internal/spaceSync's SpaceSyncer is a long-lived process (a scheduler
//   fiber, per-space fibers, a credential cache), which a stateless Worker
//   request can't host. A Durable Object can.
// - Org OAuth sessions rotate their refresh token on every refresh. Making
//   every org-credentialed call here, in one isolate, lets the OAuth
//   client's in-memory lock serialize those refreshes (see oauth.ts).
//
// It also holds the browsers' WebSocket connections (Hibernation API,
// tagged by org) and nudges them whenever an org's synced file list
// changes.
export class SyncHub extends DurableObject<Env> {
  private syncer: PromiseSpaceSyncer | undefined;
  private oauth: OAuthClient | undefined;

  constructor(ctx: DurableObjectState, env: Env) {
    super(ctx, env);
    ctx.blockConcurrencyWhile(async () => {
      if ((await ctx.storage.getAlarm()) === null) {
        await ctx.storage.setAlarm(Date.now() + ALARM_INTERVAL_MS);
      }
    });
  }

  private getOAuth(): OAuthClient {
    this.oauth ??= createOAuthClient(this.env);
    return this.oauth;
  }

  private getSyncer(): PromiseSpaceSyncer {
    if (this.syncer) return this.syncer;
    const db = getDb(this.env);
    const oauth = this.getOAuth();
    const syncer: PromiseSpaceSyncer = createSpaceSyncer({
      serviceDid: serviceDid(this.env),
      store: new DrizzleSyncStore(db),
      sink: new FileSink({
        db,
        bucket: this.env.FILES,
        fetchBlob: (space, did, cid) => syncer.getBlob(space, did, cid),
        onChange: (orgDid) => this.broadcast(orgDid),
      }),
      // Every Drop space belongs to an org, and Drop holds that org's own
      // session (that's what connecting an org is), so the space authority
      // is always the identity to mint delegation tokens as.
      delegation: {
        issue: async (space) => {
          const authority = SpaceRef.parse(space).spaceDid;
          return getDelegationToken(await oauth.restore(authority), space);
        },
      },
    });
    this.syncer = syncer;
    return syncer;
  }

  async alarm(): Promise<void> {
    // Starting the syncer (if this is a fresh instance) is all the alarm has
    // to do: its scheduler takes it from there.
    this.getSyncer();
    await this.ctx.storage.setAlarm(Date.now() + ALARM_INTERVAL_MS);
  }

  // watch starts syncing a space (idempotent).
  async watch(space: SpaceRefString): Promise<void> {
    await this.getSyncer().watch(space);
  }

  // connectOrg starts syncing every file space the org already owns. Called
  // once an org's session is in place.
  async connectOrg(orgDid: DidString): Promise<void> {
    const agent = await this.getOAuth().restore(orgDid);
    const spaces = await listOrgFileSpaces(agent, orgDid);
    const syncer = this.getSyncer();
    await Promise.all(spaces.map((space) => syncer.watch(space)));
  }

  // upload creates the file's own org space (readable by every member),
  // uploads its blob, and writes the record that names the file and keeps
  // the blob from being garbage collected. The bytes also go straight into
  // R2 under their CID, so the sink doesn't have to fetch back what we just
  // sent. The list itself is only ever filled in by the syncer: this waits
  // (briefly) for the new space's first pass rather than writing the row
  // itself.
  async upload(input: UploadInput): Promise<{ space: SpaceRefString }> {
    const agent = await this.getOAuth().restore(input.orgDid);
    const space = await createFileSpace(agent, input.orgDid);
    const { blob, cid } = await uploadBlob(agent, input.bytes, input.mimeType);
    await this.env.FILES.put(cid, input.bytes, {
      httpMetadata: { contentType: input.mimeType },
    });
    await putFileRecord(agent, input.orgDid, space, {
      name: input.name,
      blob,
      uploadedBy: input.memberDid,
    });
    const syncer = this.getSyncer();
    await syncer.watch(space);
    await Promise.race([
      syncer.awaitIdle(space),
      new Promise((resolve) => setTimeout(resolve, UPLOAD_SYNC_WAIT_MS)),
    ]);
    return { space };
  }

  // Inbound space notifications, forwarded from the Worker's XRPC routes
  // with their raw body and Authorization header (the syncer verifies the
  // service-auth JWT itself).
  async notifyWrite(body: unknown, authorization: string | undefined) {
    await this.getSyncer().notifyWrite(body, authorization);
  }

  async notifySpaceDeleted(body: unknown, authorization: string | undefined) {
    await this.getSyncer().notifySpaceDeleted(body, authorization);
  }

  // fetch accepts a browser's WebSocket, already authenticated and scoped
  // to an org by src/routes/api.live.ts (which passes the org in a header).
  // The socket is only ever written to: it carries "changed" pings, and the
  // browser refetches the list over HTTP.
  async fetch(request: Request): Promise<Response> {
    const orgDid = request.headers.get("X-Drop-Org-Did");
    if (!orgDid || request.headers.get("Upgrade") !== "websocket") {
      return new Response("expected websocket", { status: 400 });
    }
    const pair = new WebSocketPair();
    this.ctx.acceptWebSocket(pair[1], [orgDid]);
    // Make sure the syncer is running for as long as anyone's watching.
    this.getSyncer();
    return new Response(null, { status: 101, webSocket: pair[0] });
  }

  async webSocketMessage() {}

  async webSocketClose(ws: WebSocket, code: number) {
    ws.close(code);
  }

  private broadcast(orgDid: string) {
    for (const ws of this.ctx.getWebSockets(orgDid)) {
      try {
        ws.send("changed");
      } catch {
        // Already closing; the browser reconnects on its own.
      }
    }
  }
}

export function syncHub(env: Env): DurableObjectStub<SyncHub> {
  return env.SYNC_HUB.get(env.SYNC_HUB.idFromName("hub"));
}
