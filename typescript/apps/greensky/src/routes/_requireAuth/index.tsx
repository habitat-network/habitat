import { createFileRoute } from "@tanstack/react-router";
import { useState, useEffect, useRef, useCallback } from "react";
import { AuthManager } from "internal";
import {
  getPrivatePosts,
  getPostVisibility,
  getProfile,
  getProfiles,
  PrivatePost,
  type Profile,
} from "../../habitatApi";
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
  embed?: {
    $type: string;
    record?: {
      uri?: string;
      author?: { handle: string };
    };
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
    const [{ items: bskyItems, cursor: bskyCursor }, privatePosts] =
      await Promise.all([
        getBskyFeed(context.authManager),
        getPrivatePosts(context.authManager),
      ]);

    const parentDids = [
      ...new Set(
        privatePosts
          .filter((p) => p.value.reply)
          .map((p) => p.value.reply!.parent.uri.split("/")[2] ?? "")
          .filter(Boolean),
      ),
    ];
    const parentProfiles = await getProfiles(context.authManager, parentDids);
    const parentHandleByDid = new Map(
      parentDids.map((did, i) => [did, parentProfiles[i]?.handle]),
    );

    const privatePostToFeedEntry = async (
      post: PrivatePost,
    ): Promise<FeedEntry> => {
      const did = post.uri.split("/")[2];
      const privateAuthor: Profile | undefined = did
        ? await getProfile(context.authManager, did)
        : undefined;

      const granteeDids = (post.resolvedClique ?? []).slice(0, 5);
      const grantees = await getProfiles(context.authManager, granteeDids);

      const parentDid = post.value.reply?.parent.uri.split("/")[2];
      const replyToHandle = post.value.reply
        ? (parentDid ? (parentHandleByDid.get(parentDid) ?? null) : null)
        : undefined;

      return {
        uri: post.uri,
        text: post.value.text,
        createdAt: post.value.createdAt,
        kind: getPostVisibility(post, did ?? ""),
        author: privateAuthor,
        replyToHandle,
        grantees: grantees.length > 0 ? grantees : undefined,
      };
    };
    const privateEntries = await Promise.all(
      privatePosts.map(privatePostToFeedEntry),
    );

    const entries: FeedEntry[] = [
      ...privateEntries,
      ...bskyItems.map(
        ({ post, reply, reason }): FeedEntry => ({
          uri: post.uri,
          text: post.record.text,
          createdAt: post.record.createdAt,
          kind: "public",
          author: post.author,
          replyToHandle:
            reply !== undefined
              ? (reply.parent.author?.handle ?? null)
              : undefined,
          repostedByHandle:
            reason?.$type === "app.bsky.feed.defs#reasonRepost"
              ? reason.by.handle
              : undefined,
          quotedPost: quoteRepostInfo(post.embed),
        }),
      ),
    ];

    const allEntries = entries.filter((e) => e.replyToHandle === undefined);
    return {
      privateEntries: allEntries.filter((e) => e.kind !== "public"),
      bskyEntries: allEntries.filter((e) => e.kind === "public"),
      bskyCursor,
    };
  },
  component() {
    const { authManager, myProfile, isOnboarded } = Route.useRouteContext();
    const {
      privateEntries,
      bskyEntries: initialBskyEntries,
      bskyCursor: initialCursor,
    } = Route.useLoaderData();

    const [bskyEntries, setBskyEntries] =
      useState<FeedEntry[]>(initialBskyEntries);
    const [cursor, setCursor] = useState<string | undefined>(initialCursor);

    const sortByTime = (a: FeedEntry, b: FeedEntry) => {
      const aTime = a.createdAt ? new Date(a.createdAt).getTime() : 0;
      const bTime = b.createdAt ? new Date(b.createdAt).getTime() : 0;
      if (!aTime && !bTime) return 0;
      if (!aTime) return -1;
      if (!bTime) return 1;
      return bTime - aTime;
    };

    const sortedAll = [...privateEntries, ...bskyEntries].sort(sortByTime);
    const lastBskyIndex = sortedAll.reduce(
      (idx, e, i) => (e.kind === "public" ? i : idx),
      -1,
    );
    const visiblePrivateEntries =
      lastBskyIndex === -1
        ? privateEntries
        : privateEntries.filter(
            (e) => sortedAll.indexOf(e) <= lastBskyIndex,
          );

    const entries = [...visiblePrivateEntries, ...bskyEntries];
    const [isLoading, setIsLoading] = useState(false);
    const sentinelRef = useRef<HTMLDivElement>(null);

    const loadMore = useCallback(async () => {
      if (!cursor || isLoading) return;
      setIsLoading(true);
      try {
        const { items, cursor: nextCursor } = await getBskyFeed(
          authManager,
          cursor,
        );
        const newEntries = items
          .filter((item) => item.reply === undefined)
          .map(
            ({ post, reason }): FeedEntry => ({
              uri: post.uri,
              text: post.record.text,
              createdAt: post.record.createdAt,
              kind: "public",
              author: post.author,
              repostedByHandle:
                reason?.$type === "app.bsky.feed.defs#reasonRepost"
                  ? reason.by.handle
                  : undefined,
              quotedPost: quoteRepostInfo(post.embed),
            }),
          );
        setBskyEntries((prev) => [...prev, ...newEntries]);
        setCursor(nextCursor);
      } finally {
        setIsLoading(false);
      }
    }, [cursor, isLoading, authManager]);

    useEffect(() => {
      const sentinel = sentinelRef.current;
      if (!sentinel) return;
      const observer = new IntersectionObserver(
        ([entry]) => {
          if (entry?.isIntersecting) loadMore();
        },
        { threshold: 0.1 },
      );
      observer.observe(sentinel);
      return () => observer.disconnect();
    }, [loadMore]);

    return (
      <>
        <NavBar
          left={
            <li>
              <h2 style={{ color: "green", fontWeight: "normal" }}>greensky</h2>
            </li>
          }
          authManager={authManager}
          myProfile={myProfile}
          isOnboarded={isOnboarded}
        />
        <Feed entries={entries} authManager={authManager} />
        <div ref={sentinelRef} className="h-4" />
        {isLoading && (
          <p className="text-sm text-muted-foreground my-4">Loading...</p>
        )}
      </>
    );
  },
});

async function getBskyFeed(
  authManager: AuthManager,
  cursor?: string,
): Promise<{ items: BskyFeedItem[]; cursor?: string }> {
  const params = new URLSearchParams();
  params.append("limit", "10");
  if (cursor) params.append("cursor", cursor);
  const feedResponse = await authManager.fetch(
    `/xrpc/app.bsky.feed.getTimeline?${params.toString()}`,
    "GET",
  );
  const feedData: { feed: BskyFeedItem[]; cursor?: string } =
    await feedResponse.json();
  return { items: feedData.feed, cursor: feedData.cursor };
}

function quoteRepostInfo(
  embed: BskyPost["embed"],
): { bskyUrl: string; authorHandle: string } | undefined {
  if (embed?.$type !== "app.bsky.embed.record#view") return undefined;
  const handle = embed.record?.author?.handle;
  const uri = embed.record?.uri;
  if (!handle || !uri) return undefined;
  const rkey = uri.split("/").pop();
  if (!rkey) return undefined;
  return { bskyUrl: `https://bsky.app/profile/${handle}/post/${rkey}`, authorHandle: handle };
}
