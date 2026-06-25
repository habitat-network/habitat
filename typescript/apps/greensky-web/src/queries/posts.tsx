import { queryOptions } from "@tanstack/react-query";
import { AuthManager, procedure, query } from "internal";

// Each root post lives in its own space of this type, so a reply written into
// the same space inherits the root's permissions.
const SPACE_TYPE = "network.habitat.greensky.thread";
const POST_COLLECTION = "network.habitat.greensky.post";

// Mirrors network.habitat.greensky.getPosts#postView / #threadView. Apps depend
// only on the `internal` client, so the view shapes are restated here rather
// than imported from the generated `api` package.
export interface PostView {
  uri: string;
  spaceUri: string;
  author: string;
  text: string;
  createdAt: string;
  indexedAt?: string;
}

export interface Thread {
  post: PostView;
  replies: PostView[];
}

// greenskyProxyHeaders targets the greensky server via pear service proxying:
// pear validates the caller's OAuth session, signs a service-auth JWT, and
// forwards the network.habitat.greensky.* call to the server's #greensky
// endpoint, which scopes the result to the caller.
export function greenskyProxyHeaders(): Headers {
  return new Headers({
    "Atproto-Proxy": `${__GREENSKY_SERVER_DID__}#greensky`,
  });
}

export const postsQueryOptions = (authManager: AuthManager) =>
  queryOptions({
    queryKey: ["greensky", "posts"],
    queryFn: async (): Promise<Thread[]> => {
      const { threads } = await query(
        "network.habitat.greensky.getPosts",
        {},
        { authManager, headers: greenskyProxyHeaders() },
      );
      return threads;
    },
    // Poll so optimistic posts get reconciled once sap ingests them.
    refetchInterval: 3000,
  });

// createPost creates a new root post directly on pear: a dedicated space for
// the thread, then the post record inside it. The greensky server holds no
// write credential — this is the expected way to write data on Habitat.
export async function createPost(
  authManager: AuthManager,
  text: string,
): Promise<{ uri: string; spaceUri: string; createdAt: string }> {
  const createdAt = new Date().toISOString();
  const { uri: spaceUri } = await procedure(
    "network.habitat.space.createSpace",
    { type: SPACE_TYPE },
    { authManager },
  );
  const { uri } = await procedure(
    "network.habitat.space.putRecord",
    {
      space: spaceUri,
      collection: POST_COLLECTION,
      record: { text, createdAt },
    },
    { authManager },
  );
  return { uri, spaceUri, createdAt };
}

// createReply writes a reply into the root post's space so it shares the
// thread's permissions, referencing the post it answers.
export async function createReply(
  authManager: AuthManager,
  opts: { spaceUri: string; rootUri: string; parentUri: string; text: string },
): Promise<{ uri: string; createdAt: string }> {
  const createdAt = new Date().toISOString();
  const { uri } = await procedure(
    "network.habitat.space.putRecord",
    {
      space: opts.spaceUri,
      collection: POST_COLLECTION,
      record: {
        text: opts.text,
        createdAt,
        reply: { root: opts.rootUri, parent: opts.parentUri },
      },
    },
    { authManager },
  );
  return { uri, createdAt };
}
