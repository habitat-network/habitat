import type { AuthManager } from "internal";
import { query, procedure } from "internal";
import { queryOptions } from "@tanstack/react-query";
import type { GroupView } from "api/types/network/habitat/groups/defs";

// homeServerDid is the home server's DID, injected at build time via the
// __HOME_SERVER_DID__ Vite define. It falls back to the local-dev domain when
// the define is absent so the groups UI works in a plain dev server too.
const homeServerDid =
  typeof __HOME_SERVER_DID__ !== "undefined" && __HOME_SERVER_DID__
    ? __HOME_SERVER_DID__
    : "did:web:home.local.habitat.network";

// homeProxyHeaders targets the home server via pear service proxying: pear
// validates the caller's OAuth session, signs a service-auth JWT, and forwards
// the network.habitat.groups.* call to the home server's #groups service
// endpoint.
export function homeProxyHeaders(): Headers {
  return new Headers({ "Atproto-Proxy": `${homeServerDid}#groups` });
}

export type { GroupView };

// groupsListQueryOptions lists the groups the calling user belongs to (directly
// or through inherited groups), as resolved by the home server's index.
export function groupsListQueryOptions(authManager: AuthManager) {
  return queryOptions({
    queryKey: ["groups"],
    queryFn: async (): Promise<GroupView[]> => {
      const { groups } = await query(
        "network.habitat.groups.listGroups",
        {},
        { fetcher: authManager, headers: homeProxyHeaders() },
      );
      return groups;
    },
  });
}

// groupQueryOptions fetches a single group with its full membership and the
// other groups it inherits members from.
export function groupQueryOptions(group: string, authManager: AuthManager) {
  return queryOptions({
    queryKey: ["group", group],
    queryFn: (): Promise<GroupView> =>
      query(
        "network.habitat.groups.getGroup",
        { group },
        { fetcher: authManager, headers: homeProxyHeaders() },
      ),
  });
}

export function createGroup(
  authManager: AuthManager,
  name: string,
  description: string,
) {
  return procedure(
    "network.habitat.groups.createGroup",
    { name, description },
    { fetcher: authManager, headers: homeProxyHeaders() },
  );
}

// addMember adds either a user (subjectDid) or another group whose members are
// inherited (subjectGroup).
export function addMember(
  authManager: AuthManager,
  group: string,
  subject: { subjectDid: string } | { subjectGroup: string },
) {
  return procedure(
    "network.habitat.groups.addMember",
    { group, ...subject },
    { fetcher: authManager, headers: homeProxyHeaders() },
  );
}
