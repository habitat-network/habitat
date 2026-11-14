import { eq, lt } from "drizzle-orm";
import { JoseKey } from "@atproto/jwk-jose";
import type {
  InternalStateData,
  Key,
  Session,
  SessionStore,
  StateStore,
} from "@atproto/oauth-client";
import type { Db } from "./index";
import { oauthSessions, oauthStates } from "./schema";
import { seal, unseal } from "@/server/seal";

// Drizzle-backed stores for @atproto/oauth-client. The client keeps a live
// `Key` object (the DPoP keypair) in both a session and a pending state, which
// can't be JSON-serialized directly — so each value is stored with the key's
// private JWK in its place and rebuilt with JoseKey.fromJWK on the way out
// (the same approach @atproto/oauth-client-node takes).

type WithJwk<T extends { dpopKey: Key }> = Omit<T, "dpopKey"> & {
  dpopJwk: NonNullable<Key["privateJwk"]>;
};

function toStored<T extends { dpopKey: Key }>(value: T): WithJwk<T> {
  const { dpopKey, ...rest } = value;
  const dpopJwk = dpopKey.privateJwk;
  if (!dpopJwk) throw new Error("DPoP key is not a private key");
  return { ...rest, dpopJwk };
}

async function fromStored<T extends { dpopKey: Key }>(
  stored: WithJwk<T>,
): Promise<T> {
  const { dpopJwk, ...rest } = stored;
  return { ...rest, dpopKey: await JoseKey.fromJWK(dpopJwk) } as unknown as T;
}

// An authorization request that's never completed (the user closes the tab
// at the PDS) leaves its state row behind; anything older than this is swept.
const STATE_TTL_MS = 60 * 60 * 1000;

export class DrizzleStateStore implements StateStore {
  constructor(
    private db: Db,
    private secret: string,
  ) {}

  async get(key: string): Promise<InternalStateData | undefined> {
    const [row] = await this.db
      .select()
      .from(oauthStates)
      .where(eq(oauthStates.key, key))
      .limit(1);
    if (!row || row.expiresAt < Date.now()) return undefined;
    return fromStored(
      await unseal<WithJwk<InternalStateData>>(this.secret, row.value),
    );
  }

  async set(key: string, value: InternalStateData): Promise<void> {
    const now = Date.now();
    await this.db.delete(oauthStates).where(lt(oauthStates.expiresAt, now));
    const row = {
      key,
      value: await seal(this.secret, toStored(value)),
      expiresAt: now + STATE_TTL_MS,
    };
    await this.db
      .insert(oauthStates)
      .values(row)
      .onConflictDoUpdate({
        target: oauthStates.key,
        set: { value: row.value, expiresAt: row.expiresAt },
      });
  }

  async del(key: string): Promise<void> {
    await this.db.delete(oauthStates).where(eq(oauthStates.key, key));
  }
}

export class DrizzleSessionStore implements SessionStore {
  constructor(
    private db: Db,
    private secret: string,
  ) {}

  async get(did: string): Promise<Session | undefined> {
    const [row] = await this.db
      .select()
      .from(oauthSessions)
      .where(eq(oauthSessions.did, did))
      .limit(1);
    if (!row) return undefined;
    return fromStored(await unseal<WithJwk<Session>>(this.secret, row.value));
  }

  async set(did: string, value: Session): Promise<void> {
    const row = {
      did,
      value: await seal(this.secret, toStored(value)),
      updatedAt: Date.now(),
    };
    await this.db
      .insert(oauthSessions)
      .values(row)
      .onConflictDoUpdate({
        target: oauthSessions.did,
        set: { value: row.value, updatedAt: row.updatedAt },
      });
  }

  async del(did: string): Promise<void> {
    await this.db.delete(oauthSessions).where(eq(oauthSessions.did, did));
  }
}
