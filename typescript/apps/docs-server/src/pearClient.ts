import type {
  NetworkHabitatSpaceCreateSpace,
  NetworkHabitatSpaceListSpaces,
  NetworkHabitatSpacePutRecord,
  NetworkHabitatSpaceGetRecord,
  NetworkHabitatRelationshipListSubjects,
  NetworkHabitatRelationshipWriteTuple,
} from "api";
import type { DerivedConfig } from "./config";
import type { OrgClient } from "./orgClient";

export interface SpaceRef {
  uri: string;
  skey: string;
}

// Role is a space role grantable via relationship tuples. Higher roles imply
// lower ones (owner > manager > writer > reader), so "reader" expands to
// everyone with read access.
export type Role = "owner" | "manager" | "writer" | "reader";

// PearClient wraps the network.habitat.space XRPC endpoints, calling them with
// the org credential. Each document is its own space (owned by the org); the
// docs server is the sole writer of the canonical records inside it.
export class PearClient {
  private config: DerivedConfig;
  private org: OrgClient;

  constructor(config: DerivedConfig, org: OrgClient) {
    this.config = config;
    this.org = org;
  }

  orgDid(): Promise<string> {
    return this.org.orgDid();
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

  // listSpaces returns every doc space the org owns.
  async listSpaces(): Promise<SpaceRef[]> {
    const orgDid = await this.org.orgDid();
    const listed = await this.call<NetworkHabitatSpaceListSpaces.OutputSchema>(
      "network.habitat.space.listSpaces",
      "GET",
      { type: this.config.spaceType, did: orgDid },
    );
    return listed.spaces.map((s) => ({
      uri: s.uri,
      skey: s.skey || skeyOf(s.uri),
    }));
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
    const orgDid = await this.org.orgDid();
    try {
      return await this.call<NetworkHabitatSpaceGetRecord.OutputSchema>(
        "network.habitat.space.getRecord",
        "GET",
        { space, repo: orgDid, collection, rkey },
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

  // grantRole grants a user the given role on a space via a relationship tuple.
  // Unlike addMember (read|write only) this can grant the manager role, which
  // the doc creator needs so they can share the doc with others. Idempotent on
  // the server side.
  async grantRole(space: string, did: string, role: Role): Promise<void> {
    await this.call<NetworkHabitatRelationshipWriteTuple.OutputSchema>(
      "network.habitat.relationship.writeTuple",
      "POST",
      {
        subject: {
          $type: "network.habitat.relationship.defs#userSubject",
          did,
        },
        relation: role,
        object: { space },
      } satisfies NetworkHabitatRelationshipWriteTuple.InputSchema,
    );
  }

  // listReaders returns the flattened set of user DIDs that can read a space,
  // expanding usersets (e.g. org member/admin groups) and role implications.
  // Requires the org credential to hold the reader role on the space (the org
  // owns the doc spaces, so it always does).
  async listReaders(space: string): Promise<string[]> {
    const out =
      await this.call<NetworkHabitatRelationshipListSubjects.OutputSchema>(
        "network.habitat.relationship.listSubjects",
        "GET",
        { space, relation: "reader" },
      );
    return out.dids;
  }
}

function skeyOf(uri: string): string {
  const skey = uri.split("/").pop();
  if (!skey) {
    throw new Error(`unexpected space URI: ${uri}`);
  }
  return skey;
}
