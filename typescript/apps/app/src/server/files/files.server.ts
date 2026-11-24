import { type Agent, type LexValue, xrpc } from "@atproto/lex";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import { community, network } from "api";
import { deleteRecordIndexed, putRecordIndexed } from "../stream";
import { FILE_COLLECTION } from "./sink";

// Calls Drop made, and the app now makes, against the Habitat instance, each as a specific OAuth
// subject (an OAuthSession is an @atproto/lex Agent).

// FILE_SPACE_TYPE is the type of the space each uploaded file lives in.
export const FILE_SPACE_TYPE = "network.habitat.drop";

// The opensocial role every org member holds (internal/opensocial's
// MemberRoleRkey). Naming it on a file space's access record is what makes
// the file readable by the whole org.
const MEMBER_ROLE = "member";

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
// file and references its blob, into the org's own repo in the file's space,
// and returns once the group's stream has indexed it into the files table.
export function putFileRecord(
  orgDid: DidString,
  space: SpaceRefString,
  file: { name: string; blob: LexValue; uploadedBy: DidString },
) {
  return putRecordIndexed(orgDid, orgDid, {
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
  });
}

// deleteFileRecord removes a file's record from the org's repo in its space,
// and returns once the files table no longer lists it. The blob it
// referenced is left for pear to garbage collect once nothing references
// it; there's no direct blob deletion. Succeeds whether or not the record
// was still there.
export function deleteFileRecord(orgDid: DidString, space: SpaceRefString) {
  return deleteRecordIndexed(orgDid, orgDid, {
    space,
    repo: orgDid,
    collection: FILE_COLLECTION,
    rkey: "self",
  });
}
