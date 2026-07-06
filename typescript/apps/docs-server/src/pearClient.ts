import type {
  NetworkHabitatSpaceCreateSpace,
  NetworkHabitatSpacePutRecord,
  NetworkHabitatSpaceGetRecord,
  NetworkHabitatRelationshipListSubjects,
  NetworkHabitatRelationshipListObjects,
} from "api";
import type { DerivedConfig } from "./config";

// habitatDIDHeader names the DID sap should authenticate the proxied request
// as. sap looks up the OAuth session it tracks for this org DID and attaches
// the access token (and Habitat-Auth-Method header) before forwarding to pear.
const habitatDIDHeader = "Habitat-Did";

// Every org has a self space (ats://<org>/network.habitat.organization/self)
// on which all org members hold the reader role, so listSubjects on it yields
// the org's membership.
const ORG_SPACE_TYPE = "network.habitat.organization";
const SELF_SKEY = "self";
const DOCS_SPACE_TYPE = "network.habitat.docs";

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
    return `ats://${orgDid}/${DOCS_SPACE_TYPE}/${skey}`;
  }

  private async call<T>(
    org: string,
    nsid: string,
    method: "GET" | "POST",
    payload: object,
  ): Promise<T> {
    const base = `${this.config.sapUrl}/proxy/${nsid}`;
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

  // createSpace creates a new doc space owned by the given org. pear generates
  // the skey.
  async createSpace(org: string): Promise<SpaceRef> {
    const created =
      await this.call<NetworkHabitatSpaceCreateSpace.OutputSchema>(
        org,
        "network.habitat.space.createSpace",
        "POST",
        {
          type: DOCS_SPACE_TYPE,
        } satisfies NetworkHabitatSpaceCreateSpace.InputSchema,
      );
    return { uri: created.uri, skey: skeyOf(created.uri) };
  }

  // listOrgMembers returns the DIDs of the org's members: everyone holding the
  // reader role on the org's self space (org membership chains through it).
  async listOrgMembers(org: string): Promise<string[]> {
    const out =
      await this.call<NetworkHabitatRelationshipListSubjects.OutputSchema>(
        org,
        "network.habitat.relationship.listSubjects",
        "GET",
        {
          space: `ats://${org}/${ORG_SPACE_TYPE}/${SELF_SKEY}`,
          relation: "reader",
        },
      );
    return out.dids;
  }

  // listReadableSpaces returns the URIs of the doc spaces the given user holds
  // the reader role on, queried on demand. sap proxies as the org, which can
  // read every space, so the caller-visibility filter never hides results.
  async listReadableSpaces(org: string, did: string): Promise<string[]> {
    const out =
      await this.call<NetworkHabitatRelationshipListObjects.OutputSchema>(
        org,
        "network.habitat.relationship.listObjects",
        "GET",
        { did, relation: "reader", type: DOCS_SPACE_TYPE },
      );
    return out.spaces;
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
