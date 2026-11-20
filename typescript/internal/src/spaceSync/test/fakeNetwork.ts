import { TID } from "@atproto/common-web";
import { P256Keypair, Secp256k1Keypair } from "@atproto/crypto";
import { type Cid, type LexMap, type LexValue, lexToJson } from "@atproto/lex";
import {
  type SpaceCommit as Commit,
  RepoCommit,
  type SerializedRecord,
  createDelegationToken,
  createSpaceCredential,
  parseSpaceToken,
  serializeRecord,
  writeRepoStar,
  spaceHostAud,
  isP256DidKey,
  verifyCredentialSig,
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
import {
  ACCEPT_SIGNATURE_HEADER,
  COMMIT_MAC_PROFILE,
  INVALID_SPACE_MAC,
  createCommitMacHeaders,
  decapsulate,
  parseMacRequest,
} from "../../spaceMac";

// In-memory stand-in for a PLC directory, a space host and repo hosts. Every
// signature, credential and STAR uses the real @atproto/space code, so tests
// exercise real verification.

export const PLC_URL = "https://plc.test";
export const SPACE_TYPE = "com.example.board";

export interface FakeAccount {
  readonly did: DidString;
  readonly keypair: Secp256k1Keypair;
  /** `#atproto_kem` keypair; its host MACs responses about its repos with it. */
  kem: P256Keypair;
  /** Set to publish a different `#atproto_kem` than the host keys MACs with. */
  publishedKem?: P256Keypair;
  readonly pds: string;
}

interface StoredRecord extends SerializedRecord {
  readonly cid: Cid;
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
 * which is what lets a client abort a download mid-archive.
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
  readonly ref: SpaceRef;
  /** `ref` as a string, for store rows, sink views and wire bodies. */
  readonly id: SpaceRefString;
  readonly repos = new Map<string, FakeRepo>();
  readonly delisted = new Set<string>();
  readonly failingRepos = new Set<string>();
  /** Repo DID → error code its host answers listRepoOps/getRepo with. */
  readonly repoHostErrors = new Map<string, string>();
  /** Repos whose host inlines tampered record values. */
  readonly forgedValues = new Set<string>();
  /** Repos whose host pages listRepoOps forever with the same cursor. */
  readonly loopingOps = new Set<string>();
  /** Repos whose host MACs a different rev than the listRepoOps commit reports. */
  readonly corruptOpsCommits = new Set<string>();
  readonly deniedUsers = new Set<string>();
  /** Notification registrations made through authorizeSpaceSync, oldest first. */
  readonly registrations: { endpoint: string; token: string }[] = [];
  readonly credentialJtis: string[] = [];
  readonly revokedJtis = new Set<string>();
  deleted = false;
  unorderedListRepos = false;
  maxOpsPage = 1000;
  listReposPageSize = 1000;
  /** Runs after each listRepos page is computed, before it is returned (simulates writes mid-walk). */
  onListRepos: (() => Promise<void>) | undefined = undefined;
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
    );
    this.id = this.ref.toString();
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

  /** JSON body of the notifySpaceUpdate a space host would send for this repo's latest write. */
  notifySpaceUpdateBody(did: string, token: string): unknown {
    const repo = this.repos.get(did)!;
    return lexToJson({
      space: this.id,
      token,
      update: {
        $type: "com.atproto.space.defs#repoWrite",
        repo: did,
        repoRev: repo.rev,
        data: repo.commit.setHash.digest(),
        spaceRev: repo.spaceRev,
        prevSpaceRev: repo.spaceRev,
      },
    } as LexValue);
  }
}

export class FakeNetwork {
  readonly accounts = new Map<string, FakeAccount>();
  readonly spaces = new Map<string, FakeSpace>();

  async createAccount(name: string): Promise<FakeAccount> {
    const keypair = await Secp256k1Keypair.create();
    const kem = await P256Keypair.create();
    const id = (
      name.toLowerCase().replace(/[^a-z]/g, "") + "a".repeat(24)
    ).slice(0, 24);
    const account = {
      did: `did:plc:${id}` as DidString,
      keypair,
      kem,
      pds: `https://${name.toLowerCase()}.pds.test`,
    } as FakeAccount;
    this.accounts.set(account.did, account);
    return account;
  }

  createSpace(authority: FakeAccount, skey = "main"): FakeSpace {
    const space = new FakeSpace(authority, skey);
    this.spaces.set(space.id, space);
    return space;
  }

  /** What getDelegationToken on `user`'s PDS would return. */
  delegationToken(user: FakeAccount, space: string): Promise<string> {
    const authority = space.split("/")[2];
    return createDelegationToken({
      iss: user.did as DidString,
      sub: space as SpaceRefString,
      aud: spaceHostAud(authority as DidString),
      keypair: user.keypair,
    });
  }

  /** Delegation from the space authority's own session. */
  delegationTokenFor(space: string): Promise<string> {
    const fake = this.spaces.get(space);
    if (!fake)
      return Promise.reject(new Error(`no session can reach ${space}`));
    return this.delegationToken(fake.authority, space);
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
        {
          id: `${account.did}#atproto_kem`,
          type: "Multikey",
          controller: account.did,
          publicKeyMultibase: (account.publishedKem ?? account.kem)
            .did()
            .slice("did:key:".length),
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

  /**
   * The repo's current commit, MACed under the shared secret the request's
   * Accept-Signature negotiates with the author's KEM key. Returns an error
   * response for a missing or malformed request.
   */
  private macCommit(
    request: Request,
    space: FakeSpace,
    did: string,
    opts: { corrupt: boolean },
  ): { commit: Commit; macHeaders: Record<string, string> } | Response {
    const repo = space.repos.get(did)!;
    const author = this.accounts.get(did)!;
    let macRequest;
    let sharedSecret: Uint8Array;
    try {
      macRequest = parseMacRequest(
        request.headers.get(ACCEPT_SIGNATURE_HEADER),
        COMMIT_MAC_PROFILE,
      );
      if (!macRequest) return xrpcError(400, INVALID_SPACE_MAC);
      sharedSecret = decapsulate(author.kem, macRequest.keyShare);
    } catch {
      return xrpcError(400, INVALID_SPACE_MAC);
    }
    const commit = repo.commit.toCommit({
      space: space.id,
      repo: did as DidString,
      rev: repo.rev,
    });
    // A corrupt commit MACs a different rev than the one it reports.
    const maced = opts.corrupt
      ? { ...commit, rev: TID.nextStr(repo.rev) }
      : commit;
    const macHeaders = createCommitMacHeaders(maced, {
      ...macRequest,
      sharedSecret,
    });
    return { commit, macHeaders };
  }

  /** Verifies `Atproto-Space-Credential` credential + HTTP signature. Returns an error response, or undefined if authorized. */
  private async authorize(
    request: Request,
    space: FakeSpace,
    audience: string,
  ): Promise<Response | undefined> {
    const headers = Object.fromEntries(request.headers);
    const token = headers.authorization?.match(
      /^Atproto-Space-Credential (.+)$/i,
    )?.[1];
    if (!token) return xrpcError(401, "AuthMissing");
    if (headers["atproto-space-audience"] !== audience)
      return xrpcError(401, "BadAudience");
    let jti: string;
    let kid: DidString;
    try {
      const parsed = await verifySpaceToken("credential", token, {
        getSigningKey: () => space.authority.keypair.did(),
        sub: space.id,
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
      await verifyCredentialSig(headers, kid);
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
        "*/xrpc/com.atproto.space.authorizeSpaceSync",
        async ({ request }) => {
          const body = (await request.json()) as {
            space?: string;
            credentialKey?: string;
            delegationToken?: string;
            notify?: { endpoint: string; token: string };
          };
          const space = this.spaceFor(body.space ?? null);
          if (space instanceof Response) return space;
          if (new URL(request.url).origin !== space.authority.pds)
            return xrpcError(400, "WrongHost");
          const keyId = body.credentialKey;
          if (!keyId || !isP256DidKey(keyId))
            return xrpcError(400, "InvalidRequest");
          // No fake space is public: without a delegation token, answer as
          // if the space didn't exist.
          const delegation = body.delegationToken;
          if (!delegation) return xrpcError(400, "SpaceNotFound");
          try {
            const token = await verifySpaceToken("delegation", delegation, {
              getSigningKey: (iss) => this.accounts.get(iss)!.keypair.did(),
              aud: spaceHostAud(space.authority.did),
              sub: space.id,
            });
            if (space.deniedUsers.has(token.payload.iss))
              return xrpcError(403, "AccountNotAuthorized");
          } catch {
            return xrpcError(400, "InvalidDelegationToken");
          }
          const credential = await createSpaceCredential({
            iss: space.authority.did as DidString,
            sub: space.id as SpaceRefString,
            keyId: keyId as DidString,
            expiresInSec: space.credentialLifetimeSec,
            keypair: space.authority.keypair,
          });
          space.credentialJtis.push(
            parseSpaceToken("credential", credential).payload.jti,
          );
          // The registration lives as long as the credential. A real host
          // confirms the endpoint first; tests drive that step themselves.
          if (body.notify) space.registrations.push(body.notify);
          return HttpResponse.json({ credential });
        },
      ),

      http.get("*/xrpc/com.atproto.space.listWriters", async ({ request }) => {
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
            $type: "com.atproto.space.defs#repoWrite",
            repo: did,
            repoRev: repo.rev,
            spaceRev: repo.spaceRev,
            data: repo.commit.setHash.digest(),
          })),
          cursor: rows.at(-1)![1].spaceRev,
        };
        await space.onListRepos?.();
        return json(response);
      }),

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
        const hostError = space.repoHostErrors.get(did);
        if (hostError) return xrpcError(400, hostError);
        if (space.loopingOps.has(did))
          return json({ ops: [], cursor: "again" });
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
        const maced = last
          ? this.macCommit(request, space, did, {
              corrupt: space.corruptOpsCommits.has(did),
            })
          : undefined;
        if (maced instanceof Response) return maced;
        const commit = maced?.commit;
        const response = json({
          ops: page.map((op) => {
            const current = repo.records.get(`${op.collection}/${op.rkey}`);
            // Only the current value for a path is inlined; stale ones are omitted.
            const value =
              op.cid && current?.cid.toString() === op.cid
                ? current.record
                : undefined;
            const served =
              value && space.forgedValues.has(did)
                ? { ...value, forged: true }
                : value;
            return { ...op, ...(served ? { value: served } : {}) };
          }),
          ...(commit ? { commit } : {}),
          ...(last ? {} : { cursor: String(start + limit) }),
        });
        for (const [name, value] of Object.entries(maced?.macHeaders ?? {}))
          response.headers.set(name, value);
        return response;
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
        const hostError = space.repoHostErrors.get(did);
        if (hostError) return xrpcError(400, hostError);
        const maced = this.macCommit(request, space, did, { corrupt: false });
        if (maced instanceof Response) return maced;
        // A STAR archive requires its records in strict path order.
        const records = [...repo.records.entries()]
          .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
          .map(([, record]) => record);
        const star = writeRepoStar(maced.commit, records);
        return new HttpResponse(streamFrom(star), {
          headers: {
            "content-type": "application/octet-stream",
            ...maced.macHeaders,
          },
        });
      }),
    ];
  }
}
