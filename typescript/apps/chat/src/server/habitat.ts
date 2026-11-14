import { type Agent, xrpc } from "@atproto/lex";
import { type DidString, SpaceRef, type SpaceRefString } from "@atproto/syntax";
import { com, community, network } from "api";
import { CHANNEL, CHANNEL_SPACE_TYPE } from "@/habitat/app";

// Calls Chat makes against the Habitat instance, each as a specific OAuth
// subject (an OAuthSession is an @atproto/lex Agent). Reading and writing
// messages isn't here: that goes through the Habitat app runtime
// (src/habitat/functions.ts).

// The opensocial role every org member holds (internal/opensocial's
// MemberRoleRkey). Naming it on a channel's access record is what opens the
// channel to the whole org.
export const MEMBER_ROLE = "member";

// membersSpace is the org's community.opensocial.members space, which holds
// the membership records the slice's ACL tables are built from.
export function membersSpace(orgDid: DidString): SpaceRefString {
  return new SpaceRef(
    orgDid,
    "community.opensocial.members",
    "self",
  ).toString();
}

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

// listOrgChannelSpaces lists every channel space an org owns, so connecting
// an org (and the cron trigger after that) can start syncing channels
// created before, or outside of, this deployment.
export async function listOrgChannelSpaces(
  orgAgent: Agent,
  orgDid: DidString,
): Promise<SpaceRefString[]> {
  const { body } = await xrpc(orgAgent, network.habitat.space.listSpaces.main, {
    params: { did: orgDid, spaceType: CHANNEL_SPACE_TYPE },
  });
  return body.spaces.map((s) => s.uri as SpaceRefString);
}

// createChannelSpace creates a new space for one channel under the org,
// readable and writable by the given roles. Must be called with the org's
// own session: community.opensocial.createSpace accepts OAuth only for the
// community DID itself.
export async function createChannelSpace(
  orgAgent: Agent,
  orgDid: DidString,
  roles: string[],
): Promise<SpaceRefString> {
  const { body } = await xrpc(orgAgent, community.opensocial.createSpace.main, {
    body: { org: orgDid, type: CHANNEL_SPACE_TYPE, roles },
  });
  return body.uri as SpaceRefString;
}

// putChannelRecord writes the network.habitat.chat.channel record that
// names a channel, into the org's own repo in the channel's space.
export async function putChannelRecord(
  orgAgent: Agent,
  orgDid: DidString,
  space: SpaceRefString,
  channel: { name: string; topic?: string; createdBy: DidString },
): Promise<void> {
  await xrpc(orgAgent, com.atproto.space.putRecord.main, {
    body: {
      space,
      repo: orgDid,
      collection: CHANNEL,
      rkey: "self",
      record: {
        $type: CHANNEL,
        name: channel.name,
        ...(channel.topic ? { topic: channel.topic } : {}),
        createdBy: channel.createdBy,
        createdAt: new Date().toISOString(),
      },
    },
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
