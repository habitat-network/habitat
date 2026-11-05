/** Thin client for the unauthenticated pear endpoints the tests use to set up state. */

export interface CreateOrgInput {
  /** Becomes the org's handle domain: <adminHandle>.<subdomain>.<instance domain> */
  subdomain: string;
  name?: string;
  adminHandle?: string;
  adminPassword?: string;
  contactEmail?: string;
}

export interface Org {
  orgId: string;
  adminDid: string;
  /** Full handle to sign in with, e.g. admin.acme.localhost */
  adminHandle: string;
  adminPassword: string;
  name: string;
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

  async createOrg(input: CreateOrgInput): Promise<Org> {
    const adminPassword = input.adminPassword ?? "correct-horse-battery";
    const res = await fetch(`${this.baseUrl}/xrpc/network.habitat.org.create`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        name: input.name ?? input.subdomain,
        admin_handle: input.adminHandle ?? "admin",
        admin_password: adminPassword,
        contact_email:
          input.contactEmail ?? `admin@${input.subdomain}.example.com`,
        login_method: "password",
        handle_subdomain: input.subdomain,
      }),
    });
    if (!res.ok)
      throw new Error(`createOrg ${res.status}: ${await res.text()}`);
    const body = (await res.json()) as {
      org_id: string;
      admin_did: string;
      admin_handle: string;
      name: string;
    };
    return {
      orgId: body.org_id,
      adminDid: body.admin_did,
      adminHandle: body.admin_handle,
      adminPassword,
      name: body.name,
    };
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
