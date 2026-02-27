import { createFileRoute } from "@tanstack/react-router";
import { AuthManager } from "internal/authManager.js";
import {
  getPrivatePosts,
  getPostVisibility,
  getProfile,
  getProfiles,
  PrivatePost,
  type Profile,
} from "../../habitatApi";
import { ensureCacheFresh } from "../../privatePostCache";
import { type FeedEntry, Feed } from "../../Feed";
import { NavBar } from "../../components/NavBar";
import { useState, useEffect, useRef, useCallback, useMemo } from "react";

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

function bskyItemToFeedEntry({ post, reply, reason }: BskyFeedItem): FeedEntry {
  return {
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
  };
}

export const Route = createFileRoute("/_requireAuth/")({
  async loader({ context }): Promise<{
    blueskyEntries: FeedEntry[];
    cursor: string | undefined;
    privateEntries: FeedEntry[];
  }> {
    await ensureCacheFresh(context.authManager);
    const [{ items: bskyItems, cursor }, privatePosts] = await Promise.all([
      getBskyFeed(context.authManager),
      getPrivatePosts(context.authManager),
    ]);

    const privatePostToFeedEntry = async (
      post: PrivatePost,
    ): Promise<FeedEntry> => {
      const did = post.uri.split("/")[2];
      const privateAuthor: Profile | undefined = did
        ? await getProfile(context.authManager, did)
        : undefined;

      const granteeDids = (post.resolvedClique ?? []).slice(0, 5);
      const grantees = await getProfiles(context.authManager, granteeDids);

      return {
        uri: post.uri,
        text: post.value.text,
        createdAt: post.value.createdAt,
        kind: getPostVisibility(post, did ?? ""),
        author: privateAuthor,
        replyToHandle: post.value.reply !== undefined ? null : undefined,
        grantees: grantees.length > 0 ? grantees : undefined,
      };
    };

    const privateEntries = (
      await Promise.all(privatePosts.map(privatePostToFeedEntry))
    ).filter((e) => e.replyToHandle === undefined);

    const blueskyEntries = bskyItems
      .map(bskyItemToFeedEntry)
      .filter((e) => e.replyToHandle === undefined);

    return { blueskyEntries, cursor, privateEntries };
  },
  component() {
    const { authManager, myProfile, isOnboarded } = Route.useRouteContext();
    const loaderData = Route.useLoaderData() as {
      blueskyEntries: FeedEntry[];
      cursor: string | undefined;
      privateEntries: FeedEntry[];
    };

    const [blueskyEntries, setBlueskyEntries] = useState<FeedEntry[]>(
      loaderData.blueskyEntries,
    );
    const [cursor, setCursor] = useState<string | undefined>(loaderData.cursor);
    const [isLoadingMore, setIsLoadingMore] = useState(false);
    const sentinelRef = useRef<HTMLDivElement>(null);

    // Reset state when the loader runs again (e.g. page navigation)
    useEffect(() => {
      setBlueskyEntries(loaderData.blueskyEntries);
      setCursor(loaderData.cursor);
    }, [loaderData]);

    const loadMore = useCallback(async () => {
      if (!cursor || isLoadingMore) return;
      setIsLoadingMore(true);
      try {
        const { items, cursor: nextCursor } = await getBskyFeed(
          authManager,
          cursor,
        );
        const newEntries = items
          .map(bskyItemToFeedEntry)
          .filter((e) => e.replyToHandle === undefined);
        setBlueskyEntries((prev) => [...prev, ...newEntries]);
        setCursor(nextCursor);
      } finally {
        setIsLoadingMore(false);
      }
    }, [cursor, isLoadingMore, authManager]);

    useEffect(() => {
      const el = sentinelRef.current;
      if (!el) return;
      const observer = new IntersectionObserver(
        ([entry]) => {
          if (entry.isIntersecting) loadMore();
        },
        { rootMargin: "400px" },
      );
      observer.observe(el);
      return () => observer.disconnect();
    }, [loadMore]);

    // Show private posts only within the chronological window already loaded from Bluesky.
    // As more Bluesky pages load, the oldest timestamp moves earlier, revealing more private posts.
    const visibleEntries = useMemo(() => {
      const oldestBskyTime = blueskyEntries.reduce<number | null>((min, e) => {
        if (!e.createdAt) return min;
        const t = new Date(e.createdAt).getTime();
        return min === null ? t : Math.min(min, t);
      }, null);

      const visiblePrivate =
        oldestBskyTime === null
          ? loaderData.privateEntries
          : loaderData.privateEntries.filter((e) => {
              if (!e.createdAt) return false;
              return new Date(e.createdAt).getTime() >= oldestBskyTime;
            });

      return [...blueskyEntries, ...visiblePrivate];
    }, [blueskyEntries, loaderData.privateEntries]);

    return (
      <>
        <NavBar
          left={
            <li>
              <h2 style={{ color: "#2A7047", fontWeight: "normal" }}>
                greensky by <a href="https://habitat.network">habitat 🌱</a>
              </h2>
            </li>
          }
          authManager={authManager}
          myProfile={myProfile}
          isOnboarded={isOnboarded}
        />
        <Feed entries={visibleEntries} />
        {cursor && <div ref={sentinelRef} style={{ height: 1 }} />}
        {isLoadingMore && (
          <p aria-busy="true" style={{ textAlign: "center" }}>
            Loading more posts…
          </p>
        )}
        {!cursor && !isLoadingMore && blueskyEntries.length > 0 && (
          <p
            style={{
              textAlign: "center",
              color: "rgba(24, 12, 0, 0.4)",
              padding: "1rem 0 2rem",
            }}
          >
            You're all caught up
          </p>
        )}
      </>
    );
  },
});

async function getBskyFeed(
  authManager: AuthManager,
  cursor?: string,
): Promise<{ items: BskyFeedItem[]; cursor: string | undefined }> {
  const params = new URLSearchParams();
  params.append("limit", "10");
  if (cursor) params.append("cursor", cursor);
  const feedResponse = await authManager.fetch(
    `/xrpc/app.bsky.feed.getTimeline?${params.toString()}`,
    "GET",
    null,
    new Headers(),
  );
  const feedData: { feed: BskyFeedItem[]; cursor?: string } =
    await feedResponse.json();
  return { items: feedData.feed, cursor: feedData.cursor };
}
