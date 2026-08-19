// habitatDIDHeader/habitatSessionHeader name the headers sap's /proxy/<nsid>
// handler expects (cmd/sap/server.go): the DID to authenticate as, and the
// OAuth session ID sap tracks for that DID.
const habitatDIDHeader = "Habitat-Did";
const habitatSessionHeader = "Habitat-Session";

function sapInternalUrl(): string {
  const url = process.env.CHALK_SAP_INTERNAL_URL;
  if (!url) throw new Error("CHALK_SAP_INTERNAL_URL is not set");
  return url;
}

// startLogin asks sap to begin an atproto OAuth flow for handle, telling it
// to redirect the browser back to chalk's /session/callback (with the
// resolved DID) once the PDS OAuth handshake completes. Returns the
// PDS-authorize URL the browser should be sent to next.
export async function startLogin(handle: string): Promise<string> {
  const base = process.env.CHALK_BASE_URL;
  if (!base) throw new Error("CHALK_BASE_URL is not set");
  const res = await fetch(`${sapInternalUrl()}/org/add`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      handle,
      return_to: `${base}/session/callback`,
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
// /proxy/<nsid>, which resumes the OAuth session sap tracks for
// (did, sessionId) and attaches the access token.
export class SapClient {
  constructor(
    private did: string,
    private sessionId: string,
  ) {}

  async call<T>(
    nsid: string,
    method: "GET" | "POST",
    payload: Record<string, unknown>,
  ): Promise<T> {
    const base = `${sapInternalUrl()}/proxy/${nsid}`;
    let url = base;
    let body: string | undefined;
    const headers: Record<string, string> = {
      [habitatDIDHeader]: this.did,
      [habitatSessionHeader]: this.sessionId,
    };
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
    return (await res.json()) as T;
  }

  // uploadBlob uploads raw bytes to the member's own repo via
  // network.habitat.repo.uploadBlob, returning the blob reference to embed
  // in a subsequent putRecord call.
  async uploadBlob(
    bytes: Uint8Array,
    mimeType: string,
  ): Promise<{ blob: unknown; cid: string }> {
    const res = await fetch(
      `${sapInternalUrl()}/proxy/network.habitat.repo.uploadBlob`,
      {
        method: "POST",
        headers: {
          [habitatDIDHeader]: this.did,
          [habitatSessionHeader]: this.sessionId,
          "content-type": mimeType,
        },
        body: bytes as BodyInit,
      },
    );
    if (!res.ok) {
      throw new Error(`uploadBlob failed (${res.status}): ${await res.text()}`);
    }
    return (await res.json()) as { blob: unknown; cid: string };
  }
}
