import type {
  NetworkHabitatSpaceCreateSpace,
  NetworkHabitatSpacePutRecord,
  NetworkHabitatSpaceGetRecord,
  NetworkHabitatRelationshipWriteTuple,
  NetworkHabitatRelationshipListSubjects,
  NetworkHabitatRelationshipListObjects,
  NetworkHabitatRelationshipCheck,
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

export type Role = "owner" | "manager" | "writer" | "reader";

export interface SpaceRef {
  uri: string;
  skey: string;
}

// PearClient wraps the network.habitat.space XRPC endpoints. It holds no
// credential of its own; every call is routed through sap's proxy (POST
// /proxy/<nsid>) with the DID to authenticate as in the Habitat-Did header, and
// sap attaches that DID's OAuth token. The auth DID is passed per call, so one
// docs server can act for any org — or user — sap has a session for. It also
// bootstraps sap's org- and user-login OAuth flows.
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

  // startUserLogin kicks off sap's user OAuth flow for a handle, telling sap to
  // redirect the browser back to redirectUrl (this server's session callback)
  // once the user's session is stored. Returns the PDS authorization URL.
  async startUserLogin(handle: string, redirectUrl: string): Promise<string> {
    const res = await fetch(`${this.config.sapUrl}/session/add`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ handle, redirect: redirectUrl }),
    });
    if (!res.ok) {
      throw new Error(`start user login failed (${res.status})`);
    }
    const { redirect_url } = (await res.json()) as { redirect_url: string };
    return redirect_url;
  }

  // resolveLogin exchanges the login token sap handed to the session callback
  // for the DID that authenticated.
  async resolveLogin(loginToken: string): Promise<string> {
    const res = await fetch(
      `${this.config.sapUrl}/session/get?login=${encodeURIComponent(loginToken)}`,
    );
    if (!res.ok) {
      throw new Error(`resolve login failed (${res.status})`);
    }
    const { did } = (await res.json()) as { did: string };
    return did;
  }

  // proxy forwards an arbitrary XRPC request to pear through sap, authenticated
  // as authDid. Used to serve the docsv2 frontend's space/relationship calls
  // directly (as the logged-in user) instead of the frontend proxying via pear.
  // `forward` carries the allowlisted request headers to pass through (e.g.
  // content-type, and Atproto-Proxy for service-proxied endpoints); the auth
  // DID header is added here and sap swaps in the OAuth token. Returns the raw
  // upstream Response so status and body pass through unchanged.
  async proxy(
    authDid: string,
    nsid: string,
    method: string,
    search: string,
    body: BodyInit | undefined,
    forward: Headers,
  ): Promise<Response> {
    const headers = new Headers(forward);
    headers.set(habitatDIDHeader, authDid);
    const url = `${this.config.sapUrl}/proxy/${nsid}${search}`;
    return fetch(url, { method, body, headers });
  }

  private async call<T>(
    authDid: string,
    nsid: string,
    method: "GET" | "POST",
    payload: object,
  ): Promise<T> {
    const base = `${this.config.sapUrl}/proxy/${nsid}`;
    let url = base;
    let body: string | undefined;
    const headers: Record<string, string> = { [habitatDIDHeader]: authDid };
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

  // putRecord writes a record into repo's slice of a space, authenticated as
  // authDid. pear requires authDid === repo (a caller may only write to their
  // own repo), so the CRDT records are written as the editing user (repo = the
  // user's DID) and the markdown record as the org (repo = the org's DID).
  async putRecord(
    authDid: string,
    space: string,
    repo: string,
    collection: string,
    rkey: string,
    record: Record<string, unknown>,
  ): Promise<NetworkHabitatSpacePutRecord.OutputSchema> {
    return this.call<NetworkHabitatSpacePutRecord.OutputSchema>(
      authDid,
      "network.habitat.space.putRecord",
      "POST",
      {
        space,
        collection,
        rkey,
        record,
        repo,
      } satisfies NetworkHabitatSpacePutRecord.InputSchema,
    );
  }

  async getRecord(
    authDid: string,
    space: string,
    repo: string,
    collection: string,
    rkey: string,
  ): Promise<NetworkHabitatSpaceGetRecord.OutputSchema | undefined> {
    try {
      return await this.call<NetworkHabitatSpaceGetRecord.OutputSchema>(
        authDid,
        "network.habitat.space.getRecord",
        "GET",
        { space, repo, collection, rkey },
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

  // grantRole grants a user a role on a space via a relationship tuple. sap
  // proxies as the org (the space owner), which passes writeTuple's manager
  // check. Unlike addMember (read/write only), this can grant "owner", which
  // includes the manage-members permission needed to share the doc onward.
  async grantRole(
    org: string,
    space: string,
    did: string,
    relation: "owner" | "manager" | "writer" | "reader",
  ): Promise<void> {
    await this.call<NetworkHabitatRelationshipWriteTuple.OutputSchema>(
      org,
      "network.habitat.relationship.writeTuple",
      "POST",
      {
        subject: {
          $type: "network.habitat.relationship.defs#userSubject",
          did,
        },
        relation,
        object: { space },
      } satisfies NetworkHabitatRelationshipWriteTuple.InputSchema,
    );
  }

  // check resolves whether a user holds a role on a space (through role
  // implications and usersets). Used to authorize listComments against the
  // comment space. sap proxies as the org, which holds reader on the space.
  async check(
    org: string,
    did: string,
    relation: Role,
    space: string,
  ): Promise<boolean> {
    const out = await this.call<NetworkHabitatRelationshipCheck.OutputSchema>(
      org,
      "network.habitat.relationship.check",
      "GET",
      {
        subject: did,
        relation,
        space,
      } satisfies NetworkHabitatRelationshipCheck.QueryParams,
    );
    return out.allowed;
  }
}

function skeyOf(uri: string): string {
  const skey = uri.split("/").pop();
  if (!skey) {
    throw new Error(`unexpected space URI: ${uri}`);
  }
  return skey;
}
