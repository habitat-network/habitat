// habitatDIDHeader names the header sap's /proxy/<nsid> handler expects: the
// DID to authenticate as. sap is always run with WithSingleSessionPerUser for
// chalk, so there's only ever one session per DID to resume — no session ID
// needs to be tracked or sent.
const habitatDIDHeader = "Habitat-Did";

// sapAuthHeaders builds the extra headers every call to sap's internal port
// needs. When CHALK_SAP_INTERNAL_AUTH_SECRET is configured, sap gates those
// routes behind HTTP basic auth (cmd/sap/server.go's basicAuthMiddleware);
// the username is ignored, so it stays empty. Unset (local dev, where sap
// runs without --internal-auth-secret), no header is sent.
export function sapAuthHeaders(env: Env): Record<string, string> {
  const secret = env.CHALK_SAP_INTERNAL_AUTH_SECRET;
  if (!secret) return {};
  return { Authorization: `Basic ${btoa(`:${secret}`)}` };
}

// startLogin asks sap to begin an atproto OAuth flow for handle, telling it
// to redirect the browser back to chalk's /session/callback (with the
// resolved DID) once the PDS OAuth handshake completes. Returns the
// PDS-authorize URL the browser should be sent to next.
export async function startLogin(
  env: Env,
  handle: string,
  returnPath = "/session/callback",
): Promise<string> {
  const base = env.CHALK_BASE_URL;
  if (!base) throw new Error("CHALK_BASE_URL is not set");
  if (!env.CHALK_SAP_INTERNAL_URL)
    throw new Error("CHALK_SAP_INTERNAL_URL is not set");
  const res = await fetch(`${env.CHALK_SAP_INTERNAL_URL}/session/add`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      ...sapAuthHeaders(env),
    },
    body: JSON.stringify({
      handle,
      return_to: `${base}${returnPath}`,
    }),
  });
  if (!res.ok) {
    throw new Error(
      `failed to start login (${res.status}): ${await res.text()}`,
    );
  }
  const { redirect_url } = (await res.json()) as { redirect_url: string };
  return redirect_url;
}

// SapClient makes authenticated pear calls as a specific member, via sap's
// /proxy/<nsid>, which resumes the (single) OAuth session sap tracks for did
// and attaches the access token.
export class SapClient {
  constructor(
    private env: Env,
    private did: string,
  ) {}

  // base is a getter, not a constructor-time value: `process.env` does not
  // exist on workerd, and Cloudflare's canonical way to read bindings
  // (including from module scope) is `env` from `cloudflare:workers`, read
  // per-call here rather than cached.
  private get base(): string {
    const url = this.env.CHALK_SAP_INTERNAL_URL;
    if (!url) throw new Error("CHALK_SAP_INTERNAL_URL is not set");
    return url;
  }

  async call<T>(
    nsid: string,
    method: "GET" | "POST",
    payload: Record<string, unknown>,
    opts?: { atprotoProxy?: string },
  ): Promise<T> {
    const base = `${this.base}/proxy/${nsid}`;
    let url = base;
    let body: string | undefined;
    const headers: Record<string, string> = {
      [habitatDIDHeader]: this.did,
      ...sapAuthHeaders(this.env),
    };
    if (opts?.atprotoProxy) {
      headers["Atproto-Proxy"] = opts.atprotoProxy;
    }
    if (method === "GET") {
      const qs = new URLSearchParams();
      for (const [k, v] of Object.entries(payload)) {
        if (v !== undefined && v !== null) qs.set(k, String(v));
      }
      url = `${base}?${qs.toString()}`;
    } else {
      body = JSON.stringify(payload);
      headers["content-type"] = "application/json";
    }
    const res = await fetch(url, { method, body, headers });
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

  // uploadBlob uploads raw bytes to the member's own repo via
  // network.habitat.repo.uploadBlob, returning the blob reference to embed
  // in a subsequent putRecord call.
  async uploadBlob(
    bytes: Uint8Array,
    mimeType: string,
  ): Promise<{ blob: unknown; cid: string }> {
    const res = await fetch(
      `${this.base}/proxy/network.habitat.repo.uploadBlob`,
      {
        method: "POST",
        headers: {
          [habitatDIDHeader]: this.did,
          "content-type": mimeType,
          ...sapAuthHeaders(this.env),
        },
        body: bytes as BodyInit,
      },
    );
    if (!res.ok) {
      throw new Error(`uploadBlob failed (${res.status}): ${await res.text()}`);
    }
    return (await res.json()) as { blob: unknown; cid: string };
  }

  // getBlob fetches a blob's raw bytes from a space, addressed by its CID
  // (network.habitat.space.getBlob — requires read access to the space).
  // A putRecord'd record only carries a blob *reference* (a $type: "blob"
  // object with the CID under ref.$link), not the bytes themselves, so
  // reading a member's Yjs update back out means dereferencing it here.
  async getBlob(space: string, cid: string): Promise<Uint8Array> {
    const qs = new URLSearchParams({ space, cid });
    const res = await fetch(
      `${this.base}/proxy/network.habitat.space.getBlob?${qs.toString()}`,
      {
        method: "GET",
        headers: {
          [habitatDIDHeader]: this.did,
          ...sapAuthHeaders(this.env),
        },
      },
    );
    if (!res.ok) {
      throw new Error(`getBlob failed (${res.status}): ${await res.text()}`);
    }
    return new Uint8Array(await res.arrayBuffer());
  }

  // trackSpace asks sap to start tracking spaceUri immediately (sap's
  // POST /space/track, backed by Sap.TrackSpace) instead of waiting for the
  // member's next session crawl to discover it. Needed right after creating
  // a space: sap otherwise has no way to know it exists, so nothing this
  // member (or anyone else) writes into it ever reaches sap's outbox.
  async trackSpace(spaceUri: string): Promise<void> {
    const res = await fetch(`${this.base}/space/track`, {
      method: "POST",
      headers: {
        [habitatDIDHeader]: this.did,
        "content-type": "application/json",
        ...sapAuthHeaders(this.env),
      },
      body: JSON.stringify({ space: spaceUri }),
    });
    if (!res.ok) {
      throw new Error(`trackSpace failed (${res.status}): ${await res.text()}`);
    }
  }

  // recrawl asks sap to re-run discovery for this member from scratch
  // (sap's POST /session/recrawl, backed by Sap.Recrawl) rather than waiting
  // for the next periodic re-crawl. Called right after sign-in so a member
  // whose sap session lapsed or missed writes gets caught back up
  // immediately instead of silently missing spaces/records until the next
  // scheduled crawl. No session ID is sent — chalk always runs sap with
  // WithSingleSessionPerUser, so it's optional and ignored. sap schedules
  // the crawl in the background and responds 202 immediately; this does not
  // wait for the crawl itself to finish.
  async recrawl(): Promise<void> {
    const res = await fetch(`${this.base}/session/recrawl`, {
      method: "POST",
      headers: {
        [habitatDIDHeader]: this.did,
        ...sapAuthHeaders(this.env),
      },
    });
    if (!res.ok) {
      throw new Error(`recrawl failed (${res.status}): ${await res.text()}`);
    }
  }
}
