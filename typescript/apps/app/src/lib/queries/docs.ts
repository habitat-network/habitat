import { queryOptions } from "@tanstack/react-query";
import {
  getDocOrgAccess,
  getDocSummary,
  listComments,
  listDocAccess,
  listDocs,
} from "@/server/fns/docs";

// Docs: the list, and one doc's summary, sharing and comments.
// Query options are shared between route loaders (which ensure them) and
// the components that read them, so both use the same cache entries.

export const docsQuery = (org: string) =>
  queryOptions({
    queryKey: ["docs", org],
    queryFn: () => listDocs({ data: { org } }),
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
