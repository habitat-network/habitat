import { createFileRoute } from "@tanstack/react-router";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { AuthManager } from "internal";
import {
  getPrivatePosts,
  getPostVisibility,
  getProfile,
  getProfiles,
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
    indexedAt?: string;
  };
}

interface FeedPage {
  entries: FeedEntry[];
  nextCursor: string | undefined;
}

export const Route = createFileRoute("/_requireAuth/")({
  async loader({ context }) {
    const [{ items: bskyItems, cursor: bskyCursor }, privatePosts] =
      await Promise.all([
        getBskyFeed(context.authManager),
        getPrivatePosts(context.authManager),
      ]);

    const privateEntries = await Promise.all(
      privatePosts.filter((p) => !p.value.reply).map(async (post): Promise<FeedEntry> => {
        const did = post.uri.split("/")[2] ?? "";
        const [author, grantees] = await Promise.all([
          getProfile(context.authManager, did),
          getProfiles(context.authManager, (post.resolvedClique ?? []).slice(0, 5)),
        ]);
        return {
          uri: post.uri,
          clique: post.clique,
          text: post.value.text,
          createdAt: post.value.createdAt,
          kind: getPostVisibility(post, did),
          author,
          grantees: grantees.length > 0 ? grantees : undefined,
        };
      }),
    );

    return {
      privateEntries,
      initialPage: interleavePrivateWithBsky(privateEntries, buildBskyEntries(bskyItems ?? []), bskyCursor, true),
    };
  },
  component() {
    const { authManager, myProfile, isOnboarded } = Route.useRouteContext();
    const { privateEntries, initialPage } = Route.useLoaderData();

    const { data, fetchNextPage, hasNextPage, isFetchingNextPage } =
      useInfiniteQuery({
        queryKey: ["bskyFeed", privateEntries.length],
        queryFn: ({ pageParam }) => getInterleavedFeed(authManager, privateEntries, pageParam),
        initialPageParam: undefined as string | undefined,
        getNextPageParam: (lastPage) => lastPage.nextCursor,
        initialData: {
          pages: [initialPage],
          pageParams: [undefined],
        },
      });

    const entries = (data?.pages ?? []).flatMap((page) => page.entries);
    const sentinelRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
      const sentinel = sentinelRef.current;
      if (!sentinel) return;
      const observer = new IntersectionObserver(
        ([entry]) => {
          if (entry?.isIntersecting && hasNextPage && !isFetchingNextPage) {
            fetchNextPage();
          }
        },
        { threshold: 0.1 },
      );
      observer.observe(sentinel);
      return () => observer.disconnect();
    }, [fetchNextPage, hasNextPage, isFetchingNextPage]);

    return (
      <>
        <NavBar
          left={
            <li>
              <h2 style={{ color: "green", fontWeight: "normal" }} className="hover:underline">greensky</h2>
            </li>
          }
          authManager={authManager}
          myProfile={myProfile}
          isOnboarded={isOnboarded}
        />
        <Feed entries={entries} authManager={authManager} />
        <div ref={sentinelRef} className="h-4" />
        {isFetchingNextPage && (
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

function buildBskyEntries(items: BskyFeedItem[]): FeedEntry[] {
  return items
    .filter((item) => item.reply === undefined)
    .map(({ post, reason }): FeedEntry => {
      const isRepost = reason?.$type === "app.bsky.feed.defs#reasonRepost";
      return {
        uri: post.uri,
        text: post.record.text,
        createdAt: isRepost ? (reason?.indexedAt ?? post.record.createdAt) : post.record.createdAt,
        kind: "public",
        author: post.author,
        repostedByHandle: isRepost ? reason?.by.handle : undefined,
        quotedPost: quoteRepostInfo(post.embed),
      };
    });
}

function interleavePrivateWithBsky(
  privateEntries: FeedEntry[],
  bskyEntries: FeedEntry[],
  nextCursor: string | undefined,
  isFirstPage: boolean,
): FeedPage {
  const bskyTimes = bskyEntries
    .map((e) => (e.createdAt ? new Date(e.createdAt).getTime() : 0))

  const pageMinTime = bskyTimes.length > 0 ? Math.min(...bskyTimes) : 0;
  const pageMaxTime = bskyTimes.length > 0 ? Math.max(...bskyTimes) : 0;

  const privateInRange = privateEntries.filter((e) => {
    const t = e.createdAt ? new Date(e.createdAt).getTime() : 0;
    if (isFirstPage) return t >= pageMinTime;
    return t >= pageMinTime && t < pageMaxTime;
  });

  const entries = [...privateInRange, ...bskyEntries].sort((a, b) => {
    const aTime = a.createdAt ? new Date(a.createdAt).getTime() : 0;
    const bTime = b.createdAt ? new Date(b.createdAt).getTime() : 0;
    return bTime - aTime;
  });

  return { entries, nextCursor };
}

async function getInterleavedFeed(
  authManager: AuthManager,
  privateEntries: FeedEntry[],
  bskyCursor: string | undefined,
): Promise<FeedPage> {
  const { items, cursor: nextCursor } = await getBskyFeed(authManager, bskyCursor);
  return interleavePrivateWithBsky(privateEntries, buildBskyEntries(items), nextCursor, bskyCursor === undefined);
}
