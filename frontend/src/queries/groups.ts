import type { AuthManager } from "internal";
import { xrpc, type DidString, type UriString } from "@atproto/lex";
import { queryOptions } from "@tanstack/react-query";
import { network } from "api";

// homeServerDid is the home server's DID, injected at build time via the
// __HOME_SERVER_DID__ Vite define. It falls back to the local-dev domain when
// the define is absent so the groups UI works in a plain dev server too.
const homeServerDid =
  import.meta.env.VITE_HOME_SERVER_DID ?? "did:web:home.local.habitat.network";

// homeProxyHeaders targets the home server via pear service proxying: pear
// validates the caller's OAuth session, signs a service-auth JWT, and forwards
// the network.habitat.groups.* call to the home server's #groups service
// endpoint.
export function homeProxyHeaders(): Headers {
  return new Headers({ "Atproto-Proxy": `${homeServerDid}#groups` });
}

export type GroupView = network.habitat.groups.defs.GroupView;

// groupsListQueryOptions lists the groups the calling user belongs to (directly
// or through inherited groups), as resolved by the home server's index.
export function groupsListQueryOptions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["groups"],
    queryFn: async (): Promise<GroupView[]> => {
      const response = await xrpc(
        authManager,
        network.habitat.groups.listGroups.main,
        { params: {}, headers: homeProxyHeaders() },
      );
      return response.body.groups;
    },
  });
}

// groupQueryOptions fetches a single group with its full membership and the
// other groups it inherits members from.
export function groupQueryOptions(group: string, authManager: AuthManager) {
  return queryOptions({
    queryKey: ["group", group],
    queryFn: async (): Promise<GroupView> => {
      const response = await xrpc(
        authManager,
        network.habitat.groups.getGroup.main,
        {
          params: { group: group as UriString },
          headers: homeProxyHeaders(),
        },
      );
      return response.body;
    },
  });
}

export async function createGroup(
  authManager: AuthManager,
  name: string,
  description: string,
) {
  const response = await xrpc(
    authManager,
    network.habitat.groups.createGroup.main,
    { body: { name, description }, headers: homeProxyHeaders() },
  );
  return response.body;
}

// addMember adds either a user (subjectDid) or another group whose members are
// inherited (subjectGroup).
export async function addMember(
  authManager: AuthManager,
  group: string,
  subject: { subjectDid: string } | { subjectGroup: string },
) {
  const response = await xrpc(
    authManager,
    network.habitat.groups.addMember.main,
    {
      body: {
        group: group as UriString,
        subjectDid:
          "subjectDid" in subject
            ? (subject.subjectDid as DidString)
            : undefined,
        subjectGroup:
          "subjectGroup" in subject
            ? (subject.subjectGroup as UriString)
            : undefined,
      },
      headers: homeProxyHeaders(),
    },
  );
  return response.body;
}
