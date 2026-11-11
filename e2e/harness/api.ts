/** Thin client for the unauthenticated pear endpoints the tests use to set up state. */

export interface CreateOrgInput {
  /** The org's handle label, e.g. "acme" -> acme.<instance domain>. Unique per instance. */
  handle: string;
  /** Email domain whose members sign in to the org with Google, e.g. "acme.test". Unique per instance. */
  domain?: string;
}

export interface Org {
  /** The org's DID, e.g. did:web:abc123.localhost */
  did: string;
  handle: string;
  /** Members sign in with <anything>@<domain>. */
  domain: string;
}

export class PearApi {
  /** @param address host:port pear listens on (plain HTTP). */
  constructor(readonly address: string) {}

  get baseUrl() {
    return `http://${this.address}`;
  }

  get(path: string): Promise<Response> {
    return fetch(`${this.baseUrl}${path}`);
  }

  /**
   * Creates an org whose members sign in with Google using an email at its
   * domain (network.habitat.emaildomain.createOrg, which needs no auth).
   */
  async createOrg(input: CreateOrgInput): Promise<Org> {
    const domain = input.domain ?? `${input.handle}.test`;
    const res = await fetch(
      `${this.baseUrl}/xrpc/network.habitat.emaildomain.createOrg`,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ handle: input.handle, domain }),
      },
    );
    if (!res.ok)
      throw new Error(`createOrg ${res.status}: ${await res.text()}`);
    const { org } = (await res.json()) as { org: string };
    return { did: org, handle: input.handle, domain };
  }

  /** Fetches a path with a Host header, to hit pear's host-routed endpoints (DID docs, handles). */
  async getAsHost(host: string, path: string): Promise<Response> {
    // fetch() forbids overriding Host, so go through node:http.
    const { request } = await import("node:http");
    const url = new URL(path, this.baseUrl);
    return new Promise((resolve, reject) => {
      const req = request(
        {
          host: url.hostname,
          port: url.port,
          path: url.pathname,
          headers: { host },
        },
        (res) => {
          const chunks: Buffer[] = [];
          res.on("data", (c: Buffer) => chunks.push(c));
          res.on("end", () =>
            resolve(
              new Response(Buffer.concat(chunks), {
                status: res.statusCode,
                headers: res.headers as Record<string, string>,
              }),
            ),
          );
        },
      );
      req.on("error", reject);
      req.end();
    });
  }
}
