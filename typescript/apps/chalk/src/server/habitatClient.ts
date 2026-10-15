import type { OAuthSession } from "@atproto/oauth-client";

// EmailDomainNotFoundError is thrown by startLogin when a work email can't be
// resolved to an identity, i.e. its domain isn't mapped to any org.
export class EmailDomainNotFoundError extends Error {}

// UnauthorizedError is thrown by HabitatClient when pear no longer accepts
// the DID's OAuth session: a call came back 401 (after the OAuth client's
// own refresh-and-retry), or the session couldn't be restored at all
// (revoked, or its refresh token expired). It carries the DID so the
// sign-out middleware (src/start.ts) can tell the signed-in member's own
// session apart from an org's, which failing doesn't end the member's login.
export class UnauthorizedError extends Error {
  constructor(
    readonly did: string,
    message: string,
    options?: ErrorOptions,
  ) {
    super(message, options);
  }
}

// findUnauthorizedError returns the UnauthorizedError in err's cause chain,
// if any.
export function findUnauthorizedError(
  err: unknown,
): UnauthorizedError | undefined {
  for (let e = err; e instanceof Error; e = e.cause) {
    if (e instanceof UnauthorizedError) return e;
  }
  return undefined;
}

// OAuthAppState rides through the OAuth round-trip in the `state` param and
// tells /oauth/callback which flow just finished: a member signing in, or a
// signed-in member connecting an org (see startOrgConnect in functions.ts).
export type OAuthAppState =
  { kind: "member" } | { kind: "org"; orgDid: string; memberDid: string };

// isDidNotFound reports whether err, or anything in its cause chain, carries
// the XRPC error DidNotFound — what the Habitat identity resolver returns for
// a work email at an unmapped domain (same check as internal's AuthForm). The
// OAuth client wraps resolver failures, so it's usually a cause.
function isDidNotFound(err: unknown): boolean {
  for (let e = err; e instanceof Error; e = e.cause) {
    if ((e as { xrpcError?: unknown }).xrpcError === "DidNotFound") {
      return true;
    }
  }
  return false;
}

// startLogin begins an atproto OAuth flow for loginHint (a handle, DID, or
// work email — the Habitat identity resolver accepts all three, minting an
// identity for a work email at a mapped domain), returning the
// authorization URL the browser should be sent to next. pear redirects back
// to /oauth/callback once it completes.
export async function startLogin(
  env: Env,
  loginHint: string,
  state: OAuthAppState = { kind: "member" },
): Promise<string> {
  const { createOAuthClient, OAUTH_SCOPE } = await import("./oauth");
  try {
    const url = await createOAuthClient(env).authorize(loginHint, {
      scope: OAUTH_SCOPE,
      state: JSON.stringify(state),
    });
    return url.toString();
  } catch (err) {
    if (loginHint.includes("@") && isDidNotFound(err)) {
      throw new EmailDomainNotFoundError(
        err instanceof Error ? err.message : String(err),
        { cause: err },
      );
    }
    throw err;
  }
}

// HabitatClient makes authenticated calls to the Habitat instance as a
// specific DID — a member, or an org a member has connected — using the
// OAuth session chalk holds for it (src/server/oauth.ts). Calls go to the
// session's own PDS, which proxies space and relationship calls on to the
// space's host the usual atproto way (Atproto-Proxy).
export class HabitatClient {
  private session: Promise<OAuthSession> | undefined;

  constructor(
    private env: Env,
    private did: string,
  ) {}

  // asDid returns a client for the same environment authenticated as
  // another DID's session. Used where a call has to be made as somebody
  // other than the signed-in member — creating a doc's comments space and
  // its inheritance, which only the doc owner is a manager of (see
  // ensureCommentsSpace).
  asDid(did: string): HabitatClient {
    return new HabitatClient(this.env, did);
  }

  // agent restores the DID's OAuth session once per client, lazily: most
  // clients are built per request, and some never make a call at all.
  //
  // ./oauth is imported dynamically because DocRoom imports this module, and
  // so does the Worker entry the tests load before any test file runs: a
  // static import would be evaluated then, out of reach of the vi.mock()
  // the tests swap in a fake OAuth client with (test/fakeOAuth.ts).
  private agent(): Promise<OAuthSession> {
    this.session ??= import("./oauth")
      .then(({ createOAuthClient }) =>
        createOAuthClient(this.env).restore(this.did),
      )
      .catch((err: unknown) => {
        throw new UnauthorizedError(
          this.did,
          `restoring the OAuth session for ${this.did} failed`,
          { cause: err },
        );
      });
    return this.session;
  }

  async call<T>(
    nsid: string,
    method: "GET" | "POST",
    payload: Record<string, unknown>,
    opts?: { atprotoProxy?: string },
  ): Promise<T> {
    let path = `/xrpc/${nsid}`;
    let body: string | undefined;
    const headers: Record<string, string> = {};
    if (opts?.atprotoProxy) {
      headers["atproto-proxy"] = opts.atprotoProxy;
    }
    if (method === "GET") {
      const qs = new URLSearchParams();
      for (const [k, v] of Object.entries(payload)) {
        if (v !== undefined && v !== null) qs.set(k, String(v));
      }
      path = `${path}?${qs.toString()}`;
    } else {
      body = JSON.stringify(payload);
      headers["content-type"] = "application/json";
    }
    const res = await (
      await this.agent()
    ).fetchHandler(path, { method, body, headers });
    if (res.status === 401) {
      throw new UnauthorizedError(
        this.did,
        `${nsid} failed (401): ${await res.text()}`,
      );
    }
    if (!res.ok) {
      throw new Error(`${nsid} failed (${res.status}): ${await res.text()}`);
    }
    // A procedure with no output schema (e.g.
    // network.habitat.relationship.deleteRelation) returns an empty 200
    // body. res.json() throws "Unexpected end of JSON input" on that —
    // read as text first and only parse when there's actually something to
    // parse, so a call whose return value the caller ignores doesn't throw
    // regardless.
    const text = await res.text();
    return (text ? JSON.parse(text) : undefined) as T;
  }

  // uploadBlob uploads raw bytes to the DID's own repo via
  // network.habitat.repo.uploadBlob, returning the blob reference to embed
  // in a subsequent putRecord call.
  async uploadBlob(
    bytes: Uint8Array,
    mimeType: string,
  ): Promise<{ blob: unknown; cid: string }> {
    const res = await (
      await this.agent()
    ).fetchHandler("/xrpc/network.habitat.repo.uploadBlob", {
      method: "POST",
      headers: { "content-type": mimeType },
      body: bytes as BodyInit,
    });
    if (res.status === 401) {
      throw new UnauthorizedError(
        this.did,
        `uploadBlob failed (401): ${await res.text()}`,
      );
    }
    if (!res.ok) {
      throw new Error(`uploadBlob failed (${res.status}): ${await res.text()}`);
    }
    return (await res.json()) as { blob: unknown; cid: string };
  }
}
