import { createFileRoute } from "@tanstack/react-router";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
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
    const filteredPrivateEntries = privateEntries.filter(
      (e) => e.replyToHandle === undefined,
    );

    return {
      privateEntries: filteredPrivateEntries,
      initialBskyItems: bskyItems,
      bskyCursor,
    };
  },
  component() {
    const { authManager, myProfile, isOnboarded } = Route.useRouteContext();
    const {
      privateEntries,
      initialBskyItems,
      bskyCursor: initialCursor,
    } = Route.useLoaderData();

    const { data, fetchNextPage, hasNextPage, isFetchingNextPage } =
      useInfiniteQuery({
        queryKey: ["bskyFeed"],
        queryFn: ({ pageParam }) => getBskyFeed(authManager, pageParam),
        initialPageParam: undefined as string | undefined,
        getNextPageParam: (lastPage) => lastPage.cursor,
        initialData: {
          pages: [{ items: initialBskyItems ?? [], cursor: initialCursor }],
          pageParams: [undefined],
        },
      });

    const bskyEntries: FeedEntry[] = (data?.pages ?? []).flatMap((page) =>
      page.items
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
        ),
    );

    const entries = [...privateEntries, ...bskyEntries];
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
              <h2 style={{ color: "green", fontWeight: "normal" }}>greensky</h2>
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
