import type {
  NetworkHabitatSpaceCreateSpace,
  NetworkHabitatSpacePutRecord,
  NetworkHabitatSpaceGetRecord,
  NetworkHabitatRelationshipListSubjects,
} from "api";
import type { DerivedConfig } from "./config";

// habitatDIDHeader names the DID sap should authenticate the proxied request
// as. sap looks up the OAuth session it tracks for this org DID and attaches
// the access token (and Habitat-Auth-Method header) before forwarding to pear.
const habitatDIDHeader = "Habitat-Did";

export interface SpaceRef {
  uri: string;
  skey: string;
}

// PearClient wraps the network.habitat.space XRPC endpoints. Rather than
// holding its own org credential, it routes every call through sap's proxy
// (POST /proxy/<nsid>) with the org DID in the Habitat-Did header; sap
// authenticates the request as the org. Each document is its own space (owned
// by the org); the docs server is the sole writer of its canonical records.
export class PearClient {
  private config: DerivedConfig;
  private orgDidStr: string;

  constructor(config: DerivedConfig, orgDid: string) {
    this.config = config;
    this.orgDidStr = orgDid;
  }

  orgDid(): string {
    return this.orgDidStr;
  }

  // spaceUri reconstructs a doc's space URI from its skey. Space URIs are
  // ats://<orgDid>/<type>/<skey>.
  spaceUri(skey: string, orgDid: string): string {
    return `ats://${orgDid}/${this.config.spaceType}/${skey}`;
  }

  private async call<T>(
    nsid: string,
    method: "GET" | "POST",
    payload: object,
  ): Promise<T> {
    const base = `${this.config.sapProxyUrl}/${nsid}`;
    let url = base;
    let body: string | undefined;
    const headers: Record<string, string> = {
      [habitatDIDHeader]: this.orgDidStr,
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
      const text = await res.text();
      throw new Error(`${nsid} failed (${res.status}): ${text}`);
    }
    return (await res.json()) as T;
  }

  // createSpace creates a new space for a document. pear generates the skey.
  async createSpace(): Promise<SpaceRef> {
    const created =
      await this.call<NetworkHabitatSpaceCreateSpace.OutputSchema>(
        "network.habitat.space.createSpace",
        "POST",
        {
          type: this.config.spaceType,
        } satisfies NetworkHabitatSpaceCreateSpace.InputSchema,
      );
    return { uri: created.uri, skey: skeyOf(created.uri) };
  }

  // listReaders returns the member DIDs that hold the reader role on a doc's
  // space, expanding groups and role implications. sap proxies this as the org
  // (the org owner always passes the reader check).
  async listReaders(spaceUri: string): Promise<string[]> {
    const out =
      await this.call<NetworkHabitatRelationshipListSubjects.OutputSchema>(
        "network.habitat.relationship.listSubjects",
        "GET",
        { space: spaceUri, relation: "reader" },
      );
    return out.dids;
  }

  async putRecord(
    space: string,
    collection: string,
    rkey: string,
    record: Record<string, unknown>,
  ): Promise<NetworkHabitatSpacePutRecord.OutputSchema> {
    return this.call<NetworkHabitatSpacePutRecord.OutputSchema>(
      "network.habitat.space.putRecord",
      "POST",
      {
        space,
        collection,
        rkey,
        record,
      } satisfies NetworkHabitatSpacePutRecord.InputSchema,
    );
  }

  async getRecord(
    space: string,
    collection: string,
    rkey: string,
  ): Promise<NetworkHabitatSpaceGetRecord.OutputSchema | undefined> {
    try {
      return await this.call<NetworkHabitatSpaceGetRecord.OutputSchema>(
        "network.habitat.space.getRecord",
        "GET",
        { space, repo: this.orgDidStr, collection, rkey },
      );
    } catch {
      // Record not found (or not yet replicated).
      return undefined;
    }
  }

  // addMember grants a member access to a doc's space so they can read the
  // canonical records directly via their own OAuth session.
  async addMember(
    space: string,
    did: string,
    access: "read" | "write",
  ): Promise<void> {
    try {
      await this.call<unknown>("network.habitat.space.addMember", "POST", {
        space,
        did,
        access,
      });
    } catch {
      // Already a member, or a benign race — safe to ignore.
    }
  }
}

// resolveOrgDid maps the configured org handle to its DID via the public
// atproto-did well-known endpoint (served by pear/identity, no auth). The org
// DID is opaque (did:web:<random>.<domain>), so it must be resolved rather than
// derived from the handle.
export async function resolveOrgDid(handle: string): Promise<string> {
  const res = await fetch(`https://${handle}/.well-known/atproto-did`);
  if (!res.ok) {
    throw new Error(`resolve org handle ${handle}: ${res.status}`);
  }
  return (await res.text()).trim();
}

function skeyOf(uri: string): string {
  const skey = uri.split("/").pop();
  if (!skey) {
    throw new Error(`unexpected space URI: ${uri}`);
  }
  return skey;
}
