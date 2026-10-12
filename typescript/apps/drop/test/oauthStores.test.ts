import { env } from "cloudflare:workers";
import { JoseKey } from "@atproto/jwk-jose";
import type { InternalStateData, Session } from "@atproto/oauth-client";
import { beforeEach, describe, expect, it } from "vitest";
import { getDb } from "@/db";
import { DrizzleSessionStore, DrizzleStateStore } from "@/db/oauthStores";
import { oauthSessions, oauthStates } from "@/db/schema";

describe("OAuth stores", () => {
  const db = getDb(env);
  const key = env.DROP_CREDENTIALS_KEY;

  beforeEach(async () => {
    await db.delete(oauthSessions);
    await db.delete(oauthStates);
  });

  it("round-trips a session, DPoP key included, sealed at rest", async () => {
    const store = new DrizzleSessionStore(db, key);
    const dpopKey = await JoseKey.generate(["ES256"]);
    const session = {
      dpopKey,
      authMethod: { method: "none" },
      tokenSet: {
        iss: "https://pear.example",
        sub: "did:web:org.example",
        aud: "https://pear.example",
        scope: "atproto",
        access_token: "secret-access-token",
        refresh_token: "secret-refresh-token",
        token_type: "DPoP",
      },
    } as Session;

    await store.set("did:web:org.example", session);
    const restored = await store.get("did:web:org.example");
    expect(restored?.tokenSet).toEqual(session.tokenSet);
    expect(restored?.dpopKey.privateJwk).toEqual(dpopKey.privateJwk);

    // Nothing in the stored row is readable without the key.
    const [row] = await db.select().from(oauthSessions);
    const raw = new TextDecoder().decode(row.value);
    expect(raw).not.toContain("secret-refresh-token");
    expect(raw).not.toContain((dpopKey.privateJwk as { d: string }).d);

    await store.del("did:web:org.example");
    expect(await store.get("did:web:org.example")).toBeUndefined();
  });

  it("round-trips pending state and hides expired entries", async () => {
    const store = new DrizzleStateStore(db, key);
    const state = {
      iss: "https://pear.example",
      dpopKey: await JoseKey.generate(["ES256"]),
      authMethod: { method: "none" },
      verifier: "pkce-verifier",
      appState: '{"kind":"member"}',
    } as InternalStateData;

    await store.set("state-1", state);
    expect((await store.get("state-1"))?.verifier).toBe("pkce-verifier");

    await db.update(oauthStates).set({ expiresAt: Date.now() - 1 });
    expect(await store.get("state-1")).toBeUndefined();
  });
});
