import { type Agent, type LexValue, xrpc } from "@atproto/lex";
import { type DidString, SpaceRef, type SpaceRefString } from "@atproto/syntax";
import type { Actor } from "internal";
import { com, community, network } from "api";
import { FILE_COLLECTION } from "./sink";

// Calls Drop makes against the Habitat instance, each as a specific OAuth
// subject (an OAuthSession is an @atproto/lex Agent).

// FILE_SPACE_TYPE is the type of the space each uploaded file lives in.
export const FILE_SPACE_TYPE = "network.habitat.drop";

// The opensocial role every org member holds (internal/opensocial's
// MemberRoleRkey). Naming it on a file space's access record is what makes
// the file readable by the whole org.
const MEMBER_ROLE = "member";

// listMyOrgIds lists every opensocial org the agent's subject belongs to:
// every community.opensocial.members space it holds a membership in (same
// query as chalk's listMyOrgIds and the frontend's Communities page).
export async function listMyOrgIds(agent: Agent): Promise<DidString[]> {
  const { body } = await xrpc(agent, network.habitat.space.listSpaces.main, {
    params: { spaceType: "community.opensocial.members" },
  });
  return body.spaces.map((s) => SpaceRef.parse(s.uri).spaceDid);
}

// fetchOrgName reads an org's display name off its community.opensocial
// profile record. Any member can read the org's about space, so the
// member's own agent works. Null when the read fails, so callers can fall
// back to the DID.
export async function fetchOrgName(
  agent: Agent,
  orgDid: DidString,
): Promise<string | null> {
  try {
    const { body } = await xrpc(agent, network.habitat.space.getRecord.main, {
      params: {
        space: new SpaceRef(
          orgDid,
          "community.opensocial.about",
          "self",
        ).toString(),
        repo: orgDid,
        collection: "community.opensocial.profile",
        rkey: "self",
      },
    });
    const name = (body.value as { name?: unknown }).name;
    return typeof name === "string" ? name : null;
  } catch {
    return null;
  }
}

// PROFILES_PER_REQUEST is getProfiles' per-call cap on dids (its lexicon's
// maxLength).
const PROFILES_PER_REQUEST = 100;

// fetchOrgProfiles asks Habitat's network.habitat.opensocial.getProfiles for
// the profiles of dids as members of org, the way the org's other members
// see them (a member's own memberProfile, else their public Bluesky one).
// DIDs that aren't members of org are omitted by the server, so callers must
// tolerate missing entries.
//
// Sent through the member's own PDS with an Atproto-Proxy to the org's
// Habitat service, which is what serves the opensocial lexicons; the PDS
// itself knows nothing of them.
export async function fetchOrgProfiles(
  agent: Agent,
  orgDid: DidString,
  dids: string[],
): Promise<Actor[]> {
  const unique = [...new Set(dids)] as DidString[];
  const proxied: Agent = {
    fetchHandler: (path, init) => {
      const headers = new Headers(init.headers);
      headers.set("atproto-proxy", `${orgDid}#habitat`);
      return agent.fetchHandler(path, { ...init, headers });
    },
  };
  const chunks: DidString[][] = [];
  for (let i = 0; i < unique.length; i += PROFILES_PER_REQUEST) {
    chunks.push(unique.slice(i, i + PROFILES_PER_REQUEST));
  }
  const responses = await Promise.all(
    chunks.map((chunk) =>
      xrpc(proxied, network.habitat.opensocial.getProfiles.main, {
        params: { org: orgDid, dids: chunk },
      }),
    ),
  );
  return responses.flatMap(({ body }) =>
    body.profiles.map((p) => ({
      did: p.did,
      handle: p.handle,
      displayName: p.displayName,
      avatar: p.avatarUrl,
    })),
  );
}

// listOrgFileSpaces lists every Drop file space an org owns, so connecting
// an org can start syncing files uploaded before this deployment had its
// session (e.g. by another Drop deployment).
export async function listOrgFileSpaces(
  orgAgent: Agent,
  orgDid: DidString,
): Promise<SpaceRefString[]> {
  const { body } = await xrpc(orgAgent, network.habitat.space.listSpaces.main, {
    params: { did: orgDid, spaceType: FILE_SPACE_TYPE },
  });
  return body.spaces.map((s) => s.uri as SpaceRefString);
}

// createFileSpace creates a new space for one file under the org, readable
// by every org member. Must be called with the org's own session:
// community.opensocial.createSpace accepts OAuth only for the community DID
// itself.
export async function createFileSpace(
  orgAgent: Agent,
  orgDid: DidString,
): Promise<SpaceRefString> {
  const { body } = await xrpc(orgAgent, community.opensocial.createSpace.main, {
    body: { org: orgDid, type: FILE_SPACE_TYPE, roles: [MEMBER_ROLE] },
  });
  return body.uri as SpaceRefString;
}

export interface UploadedBlob {
  // The blob ref exactly as the server returned it — embedded as-is in the
  // file record, so the reference matches what pear stored.
  blob: LexValue;
  cid: string;
}

// uploadBlob stores the bytes in the agent's repo. pear deletes an uploaded
// blob that no record references within a short window, which is why the
// upload is always followed by putFileRecord.
//
// Sent with a raw fetch rather than xrpc(): the input is the file's bytes
// with its own content type, not a lexicon-encoded body.
export async function uploadBlob(
  agent: Agent,
  bytes: Uint8Array,
  mimeType: string,
): Promise<UploadedBlob> {
  const res = await agent.fetchHandler(
    "/xrpc/network.habitat.repo.uploadBlob",
    {
      method: "POST",
      headers: { "content-type": mimeType || "application/octet-stream" },
      body: bytes as BodyInit,
    },
  );
  if (!res.ok) {
    const text = await res.text();
    if (res.status === 413) throw new FileTooLargeError(text);
    throw new Error(`uploadBlob failed (${res.status}): ${text}`);
  }
  const { blob, cid } = (await res.json()) as UploadedBlob;
  return { blob, cid };
}

export class FileTooLargeError extends Error {}

// putFileRecord writes the network.habitat.drop.file record that names the
// file and references its blob, into the org's own repo in the file's space.
export async function putFileRecord(
  orgAgent: Agent,
  orgDid: DidString,
  space: SpaceRefString,
  file: { name: string; blob: LexValue; uploadedBy: DidString },
): Promise<{ uri: string; cid: string }> {
  const { body } = await xrpc(orgAgent, com.atproto.space.putRecord.main, {
    body: {
      space,
      repo: orgDid,
      collection: FILE_COLLECTION,
      rkey: "self",
      record: {
        $type: FILE_COLLECTION,
        name: file.name,
        file: file.blob,
        uploadedBy: file.uploadedBy,
        createdAt: new Date().toISOString(),
      },
    },
  });
  return { uri: body.uri, cid: body.cid };
}

// deleteFileRecord removes a file's record from the org's repo in its space.
// The blob it referenced is left for pear to garbage collect once nothing
// references it; there's no direct blob deletion. Succeeds whether or not
// the record was still there.
export async function deleteFileRecord(
  orgAgent: Agent,
  orgDid: DidString,
  space: SpaceRefString,
): Promise<void> {
  await xrpc(orgAgent, com.atproto.space.deleteRecord.main, {
    body: { space, repo: orgDid, collection: FILE_COLLECTION, rkey: "self" },
  });
}

// getDelegationToken mints a delegation token for a space from the
// subject's own PDS — the input the syncer exchanges for a space credential.
export async function getDelegationToken(
  agent: Agent,
  space: SpaceRefString,
): Promise<string> {
  const { body } = await xrpc(
    agent,
    com.atproto.space.getDelegationToken.main,
    {
      params: { space },
    },
  );
  return body.token;
}
