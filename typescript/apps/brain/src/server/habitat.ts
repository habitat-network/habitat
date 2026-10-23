import { type Agent, xrpc } from "@atproto/lex";
import { type DidString, SpaceRef, type SpaceRefString } from "@atproto/syntax";
import { com, network } from "api";
import { MESSAGE_COLLECTION, SESSION_COLLECTION } from "./sink";

// Calls Brain makes against the Habitat instance, each as the signed-in
// member (an OAuthSession is an @atproto/lex Agent).

// CHAT_SPACE_TYPE is the type of the space each chat session lives in.
export const CHAT_SPACE_TYPE = "network.habitat.brain";

// listMyOrgIds lists every opensocial org the agent's subject belongs to:
// every community.opensocial.members space it holds a membership in (same
// query as chalk's and drop's listMyOrgIds).
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

// listMyChatSpaces lists every chat space the member owns, so signing in on a
// fresh deployment starts syncing chats created elsewhere.
export async function listMyChatSpaces(
  agent: Agent,
  did: DidString,
): Promise<SpaceRefString[]> {
  const { body } = await xrpc(agent, network.habitat.space.listSpaces.main, {
    params: { did, spaceType: CHAT_SPACE_TYPE },
  });
  return body.spaces.map((s) => s.uri as SpaceRefString);
}

// createChatSpace creates a new space for one chat session, owned by the
// member. Only the member can read it: nothing is shared.
export async function createChatSpace(
  agent: Agent,
  did: DidString,
): Promise<SpaceRefString> {
  const { body } = await xrpc(
    agent,
    network.habitat.simplespace.createSpace.main,
    { body: { did, type: CHAT_SPACE_TYPE } },
  );
  return body.uri as SpaceRefString;
}

// putSessionRecord names the chat. The session record lives under the literal
// key 'self', so writing it again renames the chat.
export async function putSessionRecord(
  agent: Agent,
  did: DidString,
  space: SpaceRefString,
  title: string,
): Promise<void> {
  await xrpc(agent, com.atproto.space.putRecord.main, {
    body: {
      space,
      repo: did,
      collection: SESSION_COLLECTION,
      rkey: "self",
      record: {
        $type: SESSION_COLLECTION,
        title,
        createdAt: new Date().toISOString(),
      },
    },
  });
}

// putMessageRecord stores one message of the chat under the given key.
export async function putMessageRecord(
  agent: Agent,
  did: DidString,
  space: SpaceRefString,
  message: { rkey: string; role: "user" | "assistant"; content: string },
): Promise<void> {
  await xrpc(agent, com.atproto.space.putRecord.main, {
    body: {
      space,
      repo: did,
      collection: MESSAGE_COLLECTION,
      rkey: message.rkey,
      record: {
        $type: MESSAGE_COLLECTION,
        role: message.role,
        content: message.content,
        createdAt: new Date().toISOString(),
      },
    },
  });
}

export interface SearchHit {
  uri: string;
  space: string;
  snippet?: string;
  value: unknown;
}

// searchOrgRecords full-text searches the spaces an org owns that the member
// can read (network.habitat.space.searchRecords), most relevant first.
export async function searchOrgRecords(
  agent: Agent,
  org: DidString,
  q: string,
  limit: number,
): Promise<SearchHit[]> {
  const { body } = await xrpc(agent, network.habitat.space.searchRecords.main, {
    params: { q, org, limit },
  });
  return body.records.map((r) => ({
    uri: r.uri,
    space: r.space,
    snippet: r.snippet,
    value: r.value,
  }));
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
