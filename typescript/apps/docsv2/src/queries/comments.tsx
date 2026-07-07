import { queryOptions } from "@tanstack/react-query";
import { AuthManager, procedure, query } from "internal";
import type { NetworkHabitatDocsListComments } from "api";
import { docsProxyHeaders } from "@/queries/docs";
import type { CommentRange } from "@/lib/anchor";

const COMMENT_COLLECTION = "network.habitat.docs.comment";

export type CommentThread = NetworkHabitatDocsListComments.CommentView;

// listCommentsQueryOptions fetches a doc's comment threads from the docs server
// (proxied through pear, same as listDocs).
export const listCommentsQueryOptions = (
  docId: string,
  authManager: AuthManager,
) =>
  queryOptions({
    queryKey: ["comments", docId],
    queryFn: async (): Promise<CommentThread[]> => {
      const { comments } = await query(
        "network.habitat.docs.listComments",
        { docId },
        { authManager, headers: docsProxyHeaders() },
      );
      return comments;
    },
  });

// createComment writes a top-level comment into the doc's comment space using
// the member's own session. The rkey is omitted so pear assigns a TID.
export async function createComment(
  authManager: AuthManager,
  commentSpace: string,
  args: { body: string; range: CommentRange; docSpace: string },
): Promise<{ uri: string }> {
  return procedure(
    "network.habitat.space.putRecord",
    {
      space: commentSpace,
      collection: COMMENT_COLLECTION,
      record: {
        body: args.body,
        createdAt: new Date().toISOString(),
        docSpace: args.docSpace,
        range: args.range,
      },
    },
    { authManager },
  );
}

// createReply writes a reply into the comment space, referencing the parent
// comment's URI and omitting the range (replies inherit the parent's anchor).
export async function createReply(
  authManager: AuthManager,
  commentSpace: string,
  args: { body: string; parent: string; docSpace: string },
): Promise<{ uri: string }> {
  return procedure(
    "network.habitat.space.putRecord",
    {
      space: commentSpace,
      collection: COMMENT_COLLECTION,
      record: {
        body: args.body,
        createdAt: new Date().toISOString(),
        docSpace: args.docSpace,
        parent: args.parent,
      },
    },
    { authManager },
  );
}
