const habitatDIDHeader = "Habitat-Did";

function requireEnv(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is not set`);
  return value;
}

export function sapAuthHeaders(): Record<string, string> {
  const secret = process.env.SAP_INTERNAL_AUTH_SECRET;
  if (!secret) return {};
  return { Authorization: `Basic ${Buffer.from(`:${secret}`).toString("base64")}` };
}

export async function startLogin(
  handle: string,
  returnPath = "/session/callback",
): Promise<string> {
  const base = requireEnv("SITE_URL");
  const sapUrl = requireEnv("SAP_INTERNAL_URL");
  const res = await fetch(`${sapUrl}/session/add`, {
    method: "POST",
    headers: { "content-type": "application/json", ...sapAuthHeaders() },
    body: JSON.stringify({ handle, return_to: `${base}${returnPath}` }),
  });
  if (!res.ok) {
    throw new Error(`failed to start login (${res.status}): ${await res.text()}`);
  }
  const { redirect_url } = (await res.json()) as { redirect_url: string };
  return redirect_url;
}

export class SapClient {
  constructor(private did: string) {}

  private get base(): string {
    return requireEnv("SAP_INTERNAL_URL");
  }

  async call<T>(
    nsid: string,
    method: "GET" | "POST",
    payload: Record<string, unknown>,
  ): Promise<T> {
    const base = `${this.base}/proxy/${nsid}`;
    let url = base;
    let body: string | undefined;
    const headers: Record<string, string> = {
      [habitatDIDHeader]: this.did,
      ...sapAuthHeaders(),
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
    const text = await res.text();
    return (text ? JSON.parse(text) : undefined) as T;
  }

  async trackSpace(spaceUri: string): Promise<void> {
    const res = await fetch(`${this.base}/space/track`, {
      method: "POST",
      headers: {
        [habitatDIDHeader]: this.did,
        "content-type": "application/json",
        ...sapAuthHeaders(),
      },
      body: JSON.stringify({ space: spaceUri }),
    });
    if (!res.ok) {
      throw new Error(`trackSpace failed (${res.status}): ${await res.text()}`);
    }
  }
}
