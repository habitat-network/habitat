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

// PearClient wraps the network.habitat.space XRPC endpoints. It holds no org
// credential; every call is routed through sap's proxy (POST /proxy/<nsid>)
// with the target org DID in the Habitat-Did header, and sap authenticates the
// request as that org. The org is passed per call, so one docs server can act
// for any org sap has a session for.
export class PearClient {
  private config: DerivedConfig;

  constructor(config: DerivedConfig) {
    this.config = config;
  }

  // spaceUri reconstructs a doc's space URI from its skey. Space URIs are
  // ats://<orgDid>/<type>/<skey>.
  spaceUri(skey: string, orgDid: string): string {
    return `ats://${orgDid}/${this.config.spaceType}/${skey}`;
  }

  private async call<T>(
    org: string,
    nsid: string,
    method: "GET" | "POST",
    payload: object,
  ): Promise<T> {
    const base = `${this.config.sapProxyUrl}/${nsid}`;
    let url = base;
    let body: string | undefined;
    const headers: Record<string, string> = { [habitatDIDHeader]: org };
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

  // createSpace creates a new space owned by the given org. pear generates the
  // skey.
  async createSpace(org: string): Promise<SpaceRef> {
    const created =
      await this.call<NetworkHabitatSpaceCreateSpace.OutputSchema>(
        org,
        "network.habitat.space.createSpace",
        "POST",
        {
          type: this.config.spaceType,
        } satisfies NetworkHabitatSpaceCreateSpace.InputSchema,
      );
    return { uri: created.uri, skey: skeyOf(created.uri) };
  }

  // listReaders returns the member DIDs that hold the reader role on a doc's
  // space, expanding groups and role implications. Proxied as the space's owning
  // org (the org owner always passes the reader check).
  async listReaders(org: string, spaceUri: string): Promise<string[]> {
    const out =
      await this.call<NetworkHabitatRelationshipListSubjects.OutputSchema>(
        org,
        "network.habitat.relationship.listSubjects",
        "GET",
        { space: spaceUri, relation: "reader" },
      );
    return out.dids;
  }

  async putRecord(
    org: string,
    space: string,
    collection: string,
    rkey: string,
    record: Record<string, unknown>,
  ): Promise<NetworkHabitatSpacePutRecord.OutputSchema> {
    return this.call<NetworkHabitatSpacePutRecord.OutputSchema>(
      org,
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
    org: string,
    space: string,
    collection: string,
    rkey: string,
  ): Promise<NetworkHabitatSpaceGetRecord.OutputSchema | undefined> {
    try {
      return await this.call<NetworkHabitatSpaceGetRecord.OutputSchema>(
        org,
        "network.habitat.space.getRecord",
        "GET",
        { space, repo: org, collection, rkey },
      );
    } catch {
      // Record not found (or not yet replicated).
      return undefined;
    }
  }

  // addMember grants a member access to a doc's space so they can read the
  // canonical records directly via their own OAuth session.
  async addMember(
    org: string,
    space: string,
    did: string,
    access: "read" | "write",
  ): Promise<void> {
    try {
      await this.call<unknown>(org, "network.habitat.space.addMember", "POST", {
        space,
        did,
        access,
      });
    } catch {
      // Already a member, or a benign race — safe to ignore.
    }
  }
}

function skeyOf(uri: string): string {
  const skey = uri.split("/").pop();
  if (!skey) {
    throw new Error(`unexpected space URI: ${uri}`);
  }
  return skey;
}
