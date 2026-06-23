import type {
  NetworkHabitatSpaceCreateSpace,
  NetworkHabitatSpaceListSpaces,
  NetworkHabitatSpacePutRecord,
  NetworkHabitatSpaceGetRecord,
} from "api";
import type { DerivedConfig } from "./config";
import type { OrgClient } from "./orgClient";

// PearClient wraps the network.habitat.space XRPC endpoints, calling them with
// the org credential. The docs server is the sole writer of the canonical doc
// record, so all writes go through here.
export class PearClient {
  private config: DerivedConfig;
  private org: OrgClient;
  private spaceUriPromise: Promise<string> | undefined;

  constructor(config: DerivedConfig, org: OrgClient) {
    this.config = config;
    this.org = org;
  }

  private async call<T>(
    nsid: string,
    method: "GET" | "POST",
    payload: object,
  ): Promise<T> {
    const base = `${this.config.pearHost}/xrpc/${nsid}`;
    let url = base;
    let body: string | undefined;
    if (method === "GET") {
      const qs = new URLSearchParams();
      for (const [k, v] of Object.entries(payload)) {
        if (v !== undefined && v !== null) qs.set(k, String(v));
      }
      url = `${base}?${qs.toString()}`;
    } else {
      body = JSON.stringify(payload);
    }
    const res = await this.org.orgFetch(url, {
      method,
      body,
      headers:
        method === "POST" ? { "content-type": "application/json" } : undefined,
    });
    if (!res.ok) {
      const text = await res.text();
      throw new Error(`${nsid} failed (${res.status}): ${text}`);
    }
    return (await res.json()) as T;
  }

  // ensureSpace returns the URI of the org's docs space, creating it on first
  // use. The result is cached for the process lifetime.
  ensureSpace(): Promise<string> {
    if (!this.spaceUriPromise) {
      this.spaceUriPromise = this.resolveSpace().catch((err) => {
        // Reset so a transient failure can be retried on the next call.
        this.spaceUriPromise = undefined;
        throw err;
      });
    }
    return this.spaceUriPromise;
  }

  private async resolveSpace(): Promise<string> {
    const orgDid = await this.org.orgDid();
    const listed = await this.call<NetworkHabitatSpaceListSpaces.OutputSchema>(
      "network.habitat.space.listSpaces",
      "GET",
      { type: this.config.spaceType, did: orgDid },
    );
    const existing = listed.spaces.find(
      (s) => s.skey === this.config.spaceSkey,
    );
    if (existing) {
      return existing.uri;
    }
    const created =
      await this.call<NetworkHabitatSpaceCreateSpace.OutputSchema>(
        "network.habitat.space.createSpace",
        "POST",
        {
          type: this.config.spaceType,
          skey: this.config.spaceSkey,
        } satisfies NetworkHabitatSpaceCreateSpace.InputSchema,
      );
    return created.uri;
  }

  // putRecord writes a doc record. When rkey is omitted (doc creation) pear
  // generates a TID and returns it in the record URI; doc keys must be valid
  // TIDs, so letting pear mint them avoids implementing a TID generator here.
  async putRecord(
    record: Record<string, unknown>,
    rkey?: string,
  ): Promise<NetworkHabitatSpacePutRecord.OutputSchema> {
    const space = await this.ensureSpace();
    const input: NetworkHabitatSpacePutRecord.InputSchema = {
      space,
      collection: "network.habitat.docs",
      record,
    };
    if (rkey !== undefined) {
      input.rkey = rkey;
    }
    return this.call<NetworkHabitatSpacePutRecord.OutputSchema>(
      "network.habitat.space.putRecord",
      "POST",
      input,
    );
  }

  async getRecord(
    rkey: string,
  ): Promise<NetworkHabitatSpaceGetRecord.OutputSchema | undefined> {
    const space = await this.ensureSpace();
    const orgDid = await this.org.orgDid();
    try {
      return await this.call<NetworkHabitatSpaceGetRecord.OutputSchema>(
        "network.habitat.space.getRecord",
        "GET",
        {
          space,
          repo: orgDid,
          collection: "network.habitat.docs",
          rkey,
        },
      );
    } catch {
      // Record not found (or not yet replicated) — treat as a fresh doc.
      return undefined;
    }
  }

  // addMember grants a member access to the docs space so they can read the
  // canonical record directly via their own OAuth session.
  async addMember(did: string, access: "read" | "write"): Promise<void> {
    const space = await this.ensureSpace();
    try {
      await this.call<unknown>("network.habitat.space.addMember", "POST", {
        space,
        did,
        access,
      });
    } catch {
      // Already a member, or a benign race — safe to ignore for create/update.
    }
  }
}
