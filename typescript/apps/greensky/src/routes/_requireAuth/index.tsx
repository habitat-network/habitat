import { createFileRoute } from "@tanstack/react-router";
import { AuthManager } from "internal/authManager.js";
import { getPrivatePosts, getPostVisibility, getProfile, PrivatePost, type Profile, type DidGranteePermission } from "../../habitatApi";
import { type FeedEntry, Feed } from "../../Feed";
import { NavBar } from "../../components/NavBar";

interface BskyAuthor {
  handle: string;
  displayName?: string;
  avatar?: string;
}

interface BskyPost {
  uri: string;
  author?: BskyAuthor;
  record: {
    text: string;
    createdAt?: string;
  };
}

interface BskyFeedItem {
  post: BskyPost;
  reply?: {
    parent: {
      uri: string;
      author?: BskyAuthor;
    };
  };
  reason?: {
    $type: string;
    by: BskyAuthor;
  };
}

export const Route = createFileRoute("/_requireAuth/")({
  async loader({ context }) {
    const [bskyItems, privatePosts] = await Promise.all([
      getBskyFeed(context.authManager),
      getPrivatePosts(context.authManager),
    ]);

    const privatePostToFeedEntry = async (post: PrivatePost): Promise<FeedEntry> => {
      const did = post.uri.split("/")[2];
      const privateAuthor: Profile | undefined = did
        ? await getProfile(context.authManager, did)
        : undefined;

      const granteeDids = (post.permissions ?? [])
        .filter((p): p is DidGranteePermission => p.$type === 'network.habitat.grantee#didGrantee')
        .slice(0, 5)
        .map(p => p.did);
      const granteeProfiles = await Promise.all(
        granteeDids.map(granteeDid => getProfile(context.authManager, granteeDid).catch(() => undefined))
      );
      const grantees = granteeProfiles
        .filter((p): p is Profile => p !== undefined && !!p.avatar)
        .map(p => ({ avatar: p.avatar!, handle: p.handle }));

      return {
        uri: post.uri,
        text: post.value.text,
        createdAt: post.value.createdAt,
        kind: getPostVisibility(post, did ?? ''),
        author: privateAuthor,
        replyToHandle: post.value.reply !== undefined ? null : undefined,
        grantees: grantees.length > 0 ? grantees : undefined,
      }
    }
    const privateEntries = await Promise.all(privatePosts.map(privatePostToFeedEntry))

    const entries: FeedEntry[] = [
      ...privateEntries,
      ...bskyItems.map(({ post, reply, reason }): FeedEntry => ({
        uri: post.uri,
        text: post.record.text,
        createdAt: post.record.createdAt,
        kind: "public",
        author: post.author,
        replyToHandle: reply !== undefined
          ? (reply.parent.author?.handle ?? null)
          : undefined,
        repostedByHandle:
          reason?.$type === "app.bsky.feed.defs#reasonRepost"
            ? reason.by.handle
            : undefined,
      })),
    ];

    return entries;
  },
  component() {
    const { authManager, myProfile, isOnboarded } = Route.useRouteContext();
    const entries = Route.useLoaderData();
    return (
      <>
        <NavBar
          left={<li><h2 style={{ color: "green", fontWeight: "normal" }}>greensky by <a href="https://habitat.network">habitat 🌱</a></h2></li>}
          authManager={authManager}
          myProfile={myProfile}
          isOnboarded={isOnboarded}
        />
        <Feed entries={entries} />
      </>
    );
  },
});

async function getBskyFeed(authManager: AuthManager): Promise<BskyFeedItem[]> {
  const headers = new Headers();
  const params = new URLSearchParams();
  params.append("limit", "10");
  const feedResponse = await authManager.fetch(
    `/xrpc/app.bsky.feed.getTimeline?${params.toString()}`,
    "GET",
    null,
    headers,
  );
  const feedData: { feed: BskyFeedItem[] } = await feedResponse.json();
  return feedData.feed;
}
