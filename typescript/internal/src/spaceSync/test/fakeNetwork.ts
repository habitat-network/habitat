import { TID } from "@atproto/common-web";
import { Secp256k1Keypair } from "@atproto/crypto";
import { type LexMap, type LexValue, lexToJson, toBase64 } from "@atproto/lex";
import {
  RepoCommit,
  type SerializedRecord,
  type SignedCommit,
  createSpaceToken,
  parseSpaceToken,
  serializeRecord,
  serializeRepo,
  spaceHostAud,
  verifySpaceSignature,
  verifySpaceToken,
} from "@atproto/space";
import {
  SpaceRef,
  type DidString,
  type NsidString,
  type RecordKeyString,
  type SpaceRefString,
} from "@atproto/syntax";
import { type HttpHandler, HttpResponse, http } from "msw";

// In-memory stand-in for a PLC directory, a space host and repo hosts. Every
// signature, credential and CAR uses the real @atproto/space code, so tests
// exercise real verification.

export const PLC_URL = "https://plc.test";
export const SPACE_TYPE = "com.example.board";

export interface FakeAccount {
  readonly did: DidString;
  readonly keypair: Secp256k1Keypair;
  readonly pds: string;
}

interface StoredRecord extends SerializedRecord {
  readonly record: LexMap;
}

interface OpLogEntry {
  readonly rev: string;
  readonly collection: string;
  readonly rkey: string;
  readonly cid: string | null;
  readonly prev: string | null;
}

class FakeRepo {
  readonly records = new Map<string, StoredRecord>();
  oplog: OpLogEntry[] = [];
  /** listRepoOps rejects a `since` older than this (simulates compaction). */
  oplogFloor = "";
  rev = "";
  spaceRev = "";
  readonly commit = new RepoCommit();
}

const xrpcError = (status: number, error: string, message = error) =>
  HttpResponse.json({ error, message }, { status });

const json = (value: unknown) =>
  HttpResponse.json(lexToJson(value as LexValue) as never);

/**
 * `ReadableStream.from` is not in this repo's TS lib surface (lib: ES2024, DOM),
 * so bridge the async iterable by hand. Cancelling the body closes the iterator,
 * which is what lets a client abort a download mid-CAR.
 */
const streamFrom = (
  source: AsyncIterable<Uint8Array>,
): ReadableStream<Uint8Array> => {
  const iterator = source[Symbol.asyncIterator]();
  return new ReadableStream<Uint8Array>({
    async pull(controller) {
      const { done, value } = await iterator.next();
      if (done) controller.close();
      else controller.enqueue(value);
    },
    async cancel() {
      await iterator.return?.(undefined);
    },
  });
};

export class FakeSpace {
  readonly ref: SpaceRefString;
  readonly repos = new Map<string, FakeRepo>();
  readonly delisted = new Set<string>();
  readonly failingRepos = new Set<string>();
  readonly corruptOpsCommits = new Set<string>();
  readonly deniedUsers = new Set<string>();
  readonly registrations: string[] = [];
  readonly credentialJtis: string[] = [];
  readonly revokedJtis = new Set<string>();
  deleted = false;
  unorderedListRepos = false;
  maxOpsPage = 1000;
  listReposPageSize = 1000;
  /** Runs after each listRepos page is computed, before it is returned (simulates writes mid-walk). */
  onListRepos: (() => Promise<void>) | undefined = undefined;
  registrationLifetimeMs = 6 * 60 * 60 * 1000;
  credentialLifetimeSec = 600;
  listReposCalls = 0;
  listRepoOpsCalls = 0;
  getRepoCalls = 0;

  constructor(
    readonly authority: FakeAccount,
    readonly skey: string,
  ) {
    // Build through the syntax package so the fake only ever holds a real SpaceRefString.
    this.ref = new SpaceRef(
      authority.did,
      SPACE_TYPE as NsidString,
      skey as RecordKeyString,
    ).toString();
  }

  async write(
    author: FakeAccount,
    collection: string,
    rkey: string,
    record: LexMap | null,
  ) {
    let repo = this.repos.get(author.did);
    if (!repo) {
      repo = new FakeRepo();
      this.repos.set(author.did, repo);
    }
    const path = `${collection}/${rkey}`;
    const prev = repo.records.get(path);
    if (prev) repo.commit.remove(prev.collection, prev.rkey, prev.cid);
    let cid: string | null = null;
    if (record) {
      const serialized = await serializeRecord(
        collection as NsidString,
        rkey as RecordKeyString,
        record,
      );
      repo.records.set(path, { ...serialized, record });
      repo.commit.add(serialized.collection, serialized.rkey, serialized.cid);
      cid = serialized.cid.toString();
    } else {
      repo.records.delete(path);
    }
    repo.rev = TID.nextStr(repo.rev || undefined);
    repo.oplog.push({
      rev: repo.rev,
      collection,
      rkey,
      cid,
      prev: prev?.cid.toString() ?? null,
    });
    repo.spaceRev = TID.nextStr(repo.rev);
    return { repoRev: repo.rev, spaceRev: repo.spaceRev };
  }

  truncateOplog(did: string): void {
    const repo = this.repos.get(did)!;
    repo.oplog = [];
    repo.oplogFloor = repo.rev;
  }

  revokeAllCredentials(): void {
    for (const jti of this.credentialJtis) this.revokedJtis.add(jti);
  }

  repoRevOf(did: string): string {
    return this.repos.get(did)!.rev;
  }

  spaceRevOf(did: string): string {
    return this.repos.get(did)!.spaceRev;
  }

  /** path → cid string, i.e. what a correct sink should hold for this repo. */
  expectedView(did: string): Map<string, string> {
    const repo = this.repos.get(did);
    return new Map(
      [...(repo?.records ?? new Map()).entries()].map(([path, r]) => [
        path,
        r.cid.toString(),
      ]),
    );
  }

  /** JSON body of the notifyWrite a space host would forward for this repo's latest write. */
  notifyWriteBody(did: string, prevSpaceRev?: string): unknown {
    const repo = this.repos.get(did)!;
    return lexToJson({
      space: this.ref,
      repo: did,
      repoRev: repo.rev,
      hash: repo.commit.setHash.digest(),
      spaceRev: repo.spaceRev,
      ...(prevSpaceRev ? { prevSpaceRev } : {}),
    } as LexValue);
  }
}

export class FakeNetwork {
  readonly accounts = new Map<string, FakeAccount>();
  readonly spaces = new Map<string, FakeSpace>();

  async createAccount(name: string): Promise<FakeAccount> {
    const keypair = await Secp256k1Keypair.create();
    const id = (
      name.toLowerCase().replace(/[^a-z]/g, "") + "a".repeat(24)
    ).slice(0, 24);
    const account = {
      did: `did:plc:${id}` as DidString,
      keypair,
      pds: `https://${name.toLowerCase()}.pds.test`,
    };
    this.accounts.set(account.did, account);
    return account;
  }

  createSpace(authority: FakeAccount, skey = "main"): FakeSpace {
    const space = new FakeSpace(authority, skey);
    this.spaces.set(space.ref, space);
    return space;
  }

  /** What getDelegationToken on `user`'s PDS would return. */
  delegationToken(user: FakeAccount, space: string): Promise<string> {
    const authority = space.split("/")[2];
    return createSpaceToken(
      "delegation",
      { iss: user.did, sub: space, aud: spaceHostAud(authority) },
      user.keypair,
    );
  }

  /** Delegation from the space authority's own session. */
  delegationTokenFor(space: string): Promise<string> {
    const fake = this.spaces.get(space);
    if (!fake)
      return Promise.reject(new Error(`no session can reach ${space}`));
    return this.delegationToken(fake.authority, space);
  }

  /** A service-auth JWT as a space authority would send with notifyWrite. */
  async serviceAuth(
    issuer: FakeAccount,
    opts: {
      aud: string;
      lxm: string;
      expSec?: number;
      signer?: Secp256k1Keypair;
    },
  ): Promise<string> {
    const enc = (v: unknown) =>
      toBase64(new TextEncoder().encode(JSON.stringify(v)), "base64url");
    const now = Math.floor(Date.now() / 1000);
    const signer = opts.signer ?? issuer.keypair;
    const head = enc({ alg: signer.jwtAlg, typ: "JWT" });
    const body = enc({
      iss: issuer.did,
      aud: opts.aud,
      lxm: opts.lxm,
      iat: now,
      exp: now + (opts.expSec ?? 60),
      jti: TID.nextStr(),
    });
    const sig = await signer.sign(new TextEncoder().encode(`${head}.${body}`));
    return `${head}.${body}.${toBase64(sig, "base64url")}`;
  }

  private didDoc(account: FakeAccount) {
    return {
      "@context": ["https://www.w3.org/ns/did/v1"],
      id: account.did,
      alsoKnownAs: [],
      verificationMethod: [
        {
          id: `${account.did}#atproto`,
          type: "Multikey",
          controller: account.did,
          publicKeyMultibase: account.keypair.did().slice("did:key:".length),
        },
      ],
      service: [
        {
          id: "#atproto_pds",
          type: "AtprotoPersonalDataServer",
          serviceEndpoint: account.pds,
        },
      ],
    };
  }

  private async signCommit(
    space: FakeSpace,
    did: string,
    opts: { corrupt: boolean },
  ): Promise<SignedCommit> {
    const repo = space.repos.get(did)!;
    const author = this.accounts.get(did)!;
    // A corrupt commit signs a different rev into the ctx than the one it reports.
    const ctxRev = opts.corrupt ? TID.nextStr(repo.rev) : repo.rev;
    const commit = await repo.commit.sign(
      { space: space.ref, author: did, rev: ctxRev },
      author.keypair,
    );
    return { ...commit, rev: repo.rev };
  }

  /** Verifies `Atproto-Space` credential + HTTP signature. Returns an error response, or undefined if authorized. */
  private async authorize(
    request: Request,
    space: FakeSpace,
    audience: string,
  ): Promise<Response | undefined> {
    const headers = Object.fromEntries(request.headers);
    const token = headers.authorization?.match(/^Atproto-Space (.+)$/)?.[1];
    if (!token) return xrpcError(401, "AuthMissing");
    if (headers["atproto-space-audience"] !== audience)
      return xrpcError(401, "BadAudience");
    let jti: string;
    let kid: DidString;
    try {
      const parsed = await verifySpaceToken("credential", token, {
        getSigningKey: () => space.authority.keypair.did(),
        sub: space.ref,
      });
      jti = parsed.payload.jti;
      kid = parsed.payload.cnf!.kid;
    } catch (error) {
      const code = (error as { code?: string }).code;
      return xrpcError(
        401,
        code === "JwtExpired" ? "JwtExpired" : "InvalidToken",
      );
    }
    if (space.revokedJtis.has(jti)) return xrpcError(401, "CredentialRevoked");
    try {
      await verifySpaceSignature(headers, kid);
    } catch {
      return xrpcError(401, "BadSignature");
    }
    return undefined;
  }

  private spaceFor(param: string | null): FakeSpace | Response {
    const space = this.spaces.get(param ?? "");
    if (!space) return xrpcError(400, "SpaceNotFound");
    if (space.deleted) return xrpcError(400, "SpaceDeleted");
    return space;
  }

  get handlers(): HttpHandler[] {
    return [
      http.get(`${PLC_URL}/:did`, ({ params }) => {
        const account = this.accounts.get(
          decodeURIComponent(String(params.did)),
        );
        return account
          ? HttpResponse.json(this.didDoc(account))
          : new HttpResponse(null, { status: 404 });
      }),

      http.post(
        "*/xrpc/com.atproto.space.getSpaceCredential",
        async ({ request }) => {
          const body = (await request.json()) as { space?: string };
          const space = this.spaceFor(body.space ?? null);
          if (space instanceof Response) return space;
          if (new URL(request.url).origin !== space.authority.pds)
            return xrpcError(400, "WrongHost");
          const headers = Object.fromEntries(request.headers);
          const delegation = headers.authorization?.match(/^Bearer (.+)$/)?.[1];
          if (!delegation) return xrpcError(401, "InvalidDelegationToken");
          let keyId: DidString;
          try {
            const token = await verifySpaceToken("delegation", delegation, {
              getSigningKey: (iss) => this.accounts.get(iss)!.keypair.did(),
              aud: spaceHostAud(space.authority.did),
              sub: space.ref,
            });
            if (space.deniedUsers.has(token.payload.iss))
              return xrpcError(403, "UserNotAuthorized");
            keyId = await verifySpaceSignature(headers);
          } catch {
            return xrpcError(400, "InvalidDelegationToken");
          }
          const credential = await createSpaceToken(
            "credential",
            {
              iss: space.authority.did,
              sub: space.ref,
              keyId,
              expiresInSec: space.credentialLifetimeSec,
            },
            space.authority.keypair,
          );
          space.credentialJtis.push(
            parseSpaceToken("credential", credential).payload.jti,
          );
          return HttpResponse.json({ credential });
        },
      ),

      http.get("*/xrpc/com.atproto.space.listRepos", async ({ request }) => {
        const url = new URL(request.url);
        const space = this.spaceFor(url.searchParams.get("space"));
        if (space instanceof Response) return space;
        space.listReposCalls++;
        const denied = await this.authorize(
          request,
          space,
          space.authority.did,
        );
        if (denied) return denied;
        const cursor = url.searchParams.get("cursor") ?? "";
        const limit = Math.min(
          Number(url.searchParams.get("limit") ?? 100),
          space.listReposPageSize,
        );
        const rows = [...space.repos.entries()]
          .filter(
            ([did, repo]) => !space.delisted.has(did) && repo.spaceRev > cursor,
          )
          .sort((a, b) => (a[1].spaceRev < b[1].spaceRev ? -1 : 1))
          .slice(0, limit);
        if (space.unorderedListRepos) rows.reverse();
        if (rows.length === 0) return HttpResponse.json({ repos: [] });
        const response = {
          repos: rows.map(([did, repo]) => ({
            did,
            repoRev: repo.rev,
            spaceRev: repo.spaceRev,
            hash: repo.commit.setHash.digest(),
          })),
          cursor: rows.at(-1)![1].spaceRev,
        };
        await space.onListRepos?.();
        return json(response);
      }),

      http.post(
        "*/xrpc/com.atproto.space.registerNotify",
        async ({ request }) => {
          const body = (await request.json()) as {
            space?: string;
            service?: string;
          };
          const space = this.spaceFor(body.space ?? null);
          if (space instanceof Response) return space;
          const denied = await this.authorize(
            request,
            space,
            space.authority.did,
          );
          if (denied) return denied;
          space.registrations.push(body.service ?? "");
          return HttpResponse.json({
            expiresAt: new Date(
              Date.now() + space.registrationLifetimeMs,
            ).toISOString(),
          });
        },
      ),

      http.get("*/xrpc/com.atproto.space.listRepoOps", async ({ request }) => {
        const url = new URL(request.url);
        const space = this.spaceFor(url.searchParams.get("space"));
        if (space instanceof Response) return space;
        space.listRepoOpsCalls++;
        const did = url.searchParams.get("repo") ?? "";
        const repo = space.repos.get(did);
        if (!repo) return xrpcError(400, "RepoNotFound");
        if (url.origin !== this.accounts.get(did)?.pds)
          return xrpcError(400, "WrongHost");
        const denied = await this.authorize(request, space, did);
        if (denied) return denied;
        if (space.failingRepos.has(did))
          return xrpcError(500, "InternalServerError");
        const since = url.searchParams.get("since") ?? "";
        if (since < repo.oplogFloor)
          return xrpcError(
            400,
            "InvalidRequest",
            "since is outside the retained oplog",
          );
        const ops = repo.oplog.filter((op) => op.rev > since);
        const start = Number(url.searchParams.get("cursor") ?? 0);
        const limit = Math.min(
          Number(url.searchParams.get("limit") ?? 100),
          space.maxOpsPage,
        );
        const page = ops.slice(start, start + limit);
        const last = start + limit >= ops.length;
        const commit = last
          ? await this.signCommit(space, did, {
              corrupt: space.corruptOpsCommits.has(did),
            })
          : undefined;
        return json({
          ops: page.map((op) => {
            const current = repo.records.get(`${op.collection}/${op.rkey}`);
            // Only the current value for a path is inlined; stale ones are omitted.
            const value =
              op.cid && current?.cid.toString() === op.cid
                ? current.record
                : undefined;
            return { ...op, ...(value ? { value } : {}) };
          }),
          ...(commit ? { commit } : {}),
          ...(last ? {} : { cursor: String(start + limit) }),
        });
      }),

      http.get("*/xrpc/com.atproto.space.getRepo", async ({ request }) => {
        const url = new URL(request.url);
        const space = this.spaceFor(url.searchParams.get("space"));
        if (space instanceof Response) return space;
        space.getRepoCalls++;
        const did = url.searchParams.get("repo") ?? "";
        const repo = space.repos.get(did);
        if (!repo) return xrpcError(400, "RepoNotFound");
        if (url.origin !== this.accounts.get(did)?.pds)
          return xrpcError(400, "WrongHost");
        const denied = await this.authorize(request, space, did);
        if (denied) return denied;
        if (space.failingRepos.has(did))
          return xrpcError(500, "InternalServerError");
        const commit = await this.signCommit(space, did, { corrupt: false });
        const car = serializeRepo(commit, repo.records.values());
        return new HttpResponse(streamFrom(car), {
          headers: { "content-type": "application/vnd.ipld.car" },
        });
      }),
    ];
  }
}
