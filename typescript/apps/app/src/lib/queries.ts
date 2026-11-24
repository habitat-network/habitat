import { queryOptions } from "@tanstack/react-query";
import {
  listMyGroups,
  getGroup,
  getMyGroupProfile,
  listMyInvites,
} from "@/server/fns/groups";
import { listConnectedApps, listMcpServers } from "@/server/fns/mcp";
import { listApps } from "@/server/fns/apps";
import {
  getDocOrgAccess,
  getDocSummary,
  listComments,
  listDocAccess,
  listDocs,
} from "@/server/fns/docs";
import { listFiles } from "@/server/fns/files";
import { listGroupInvites, listGroupMembers } from "@/server/fns/manage";

// Query options shared between route loaders (which prefetch) and the
// components that read them, so both use the same cache entries.

export const myGroupsQuery = queryOptions({
  queryKey: ["groups"],
  queryFn: () => listMyGroups(),
});

export const groupQuery = (org: string) =>
  queryOptions({
    queryKey: ["group", org],
    queryFn: () => getGroup({ data: { org } }),
  });

export const myInvitesQuery = queryOptions({
  queryKey: ["invites"],
  queryFn: () => listMyInvites(),
});

export const mcpServersQuery = (org: string) =>
  queryOptions({
    queryKey: ["mcp", org],
    queryFn: () => listMcpServers({ data: { org } }),
  });

export const connectedAppsQuery = queryOptions({
  queryKey: ["connectedApps"],
  queryFn: () => listConnectedApps(),
});

export const membersQuery = (org: string) =>
  queryOptions({
    queryKey: ["members", org],
    queryFn: () => listGroupMembers({ data: { org } }),
  });

export const groupInvitesQuery = (org: string) =>
  queryOptions({
    queryKey: ["groupInvites", org],
    queryFn: () => listGroupInvites({ data: { org } }),
  });

export const myGroupProfileQuery = (org: string) =>
  queryOptions({
    queryKey: ["myGroupProfile", org],
    queryFn: () => getMyGroupProfile({ data: { org } }),
  });

export const appsQuery = (org: string) =>
  queryOptions({
    queryKey: ["apps", org],
    queryFn: () => listApps({ data: { org } }),
  });

export const docsQuery = (org: string) =>
  queryOptions({
    queryKey: ["docs", org],
    queryFn: () => listDocs({ data: { org } }),
  });

export const filesQuery = (org: string) =>
  queryOptions({
    queryKey: ["files", org],
    queryFn: () => listFiles({ data: { org } }),
  });

export const docSummaryQuery = (org: string, docId: string) =>
  queryOptions({
    queryKey: ["docSummary", org, docId],
    queryFn: () => getDocSummary({ data: { org, docId } }),
  });

export const docAccessQuery = (org: string, docId: string) =>
  queryOptions({
    queryKey: ["docAccess", org, docId],
    queryFn: () => listDocAccess({ data: { org, docId } }),
  });

export const docOrgAccessQuery = (org: string, docId: string) =>
  queryOptions({
    queryKey: ["docOrgAccess", org, docId],
    queryFn: async () =>
      (await getDocOrgAccess({ data: { org, docId } })) ?? ("none" as const),
  });

export const commentsQuery = (org: string, docId: string) =>
  queryOptions({
    queryKey: ["comments", org, docId],
    queryFn: () => listComments({ data: { org, docId } }),
  });

// groupLabel is what to call a group: its name, else its DID.
export function groupLabel(group: { did: string; name: string | null }) {
  return group.name?.trim() || group.did;
}
