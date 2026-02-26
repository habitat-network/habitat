import { createFileRoute, Link } from "@tanstack/react-router";
import {
  getPrivatePost,
  getProfile,
  getProfiles,
  getPostVisibility,
} from "../../habitatApi";
import { type FeedEntry, Feed } from "../../Feed";
import { NavBar } from "../../components/NavBar";
import { PostReply } from "../../components/PostReply";
import { ensureCacheFresh, getPrivatePostCache } from "../../privatePostCache";

export const Route = createFileRoute("/_requireAuth/$handle/p/$rkey")({
  async loader({ context, params }) {
    await ensureCacheFresh(context.authManager);
    const [post, profile] = await Promise.all([
      getPrivatePost(context.authManager, params.handle, params.rkey),
      getProfile(context.authManager, params.handle),
    ]);

    if (!post)
      return {
        entries: [] as FeedEntry[],
        replyEntries: [] as FeedEntry[],
        postClique: undefined as string | undefined,
        postCid: "",
      };

    const authorDid = post.uri.split("/")[2] ?? "";
    const granteeDids = (post.resolvedClique ?? []).slice(0, 5);

    const cache = await getPrivatePostCache();
    const replyPosts = await cache.getReplies(post.uri);

    const replyAuthorDids = [...new Set(replyPosts.map((r) => r.authorDid))];

    const [grantees, replyAuthorProfiles] = await Promise.all([
      getProfiles(context.authManager, granteeDids),
      getProfiles(context.authManager, replyAuthorDids),
    ]);

    const replyAuthorByDid = new Map(
      replyAuthorDids.map((did, i) => [did, replyAuthorProfiles[i]]),
    );

    const entry: FeedEntry = {
      uri: post.uri,
      text: post.value.text,
      createdAt: post.value.createdAt,
      kind: getPostVisibility(post, authorDid),
      author: {
        handle: profile.handle,
        displayName: profile.displayName,
        avatar: profile.avatar,
      },
      replyToHandle: post.value.reply !== undefined ? null : undefined,
      grantees: grantees.length > 0 ? grantees : undefined,
    };

    const replyEntries: FeedEntry[] = replyPosts.map((reply) => {
      const replyAuthorDid = reply.authorDid;
      const replyAuthor = replyAuthorByDid.get(replyAuthorDid);
      return {
        uri: reply.uri,
        text: reply.value.text,
        createdAt: reply.value.createdAt,
        kind: getPostVisibility(reply, replyAuthorDid),
        author: replyAuthor
          ? { handle: replyAuthor.handle, avatar: replyAuthor.avatar }
          : undefined,
        replyToHandle: null,
      };
    });

    return { entries: [entry], replyEntries: replyEntries, postClique: post.clique, postCid: post.cid };
  },
  component() {
    const { handle } = Route.useParams();
    const { entries, replyEntries, postClique, postCid } = Route.useLoaderData() as {
      entries: FeedEntry[];
      replyEntries: FeedEntry[];
      postClique: string | undefined;
      postCid: string;
    };
    const { authManager, myProfile, isOnboarded } = Route.useRouteContext();
    return (
      <>
        <NavBar
          left={
            <>
              <li>
                <Link to="/">← Greensky</Link>
              </li>
              <li>
                Post by @{handle}
              </li>
            </>
          }
          authManager={authManager}
          myProfile={myProfile}
          isOnboarded={isOnboarded}
        />
        <Feed
          entries={entries}
          showPrivatePermalink={false}
          renderPostActions={(entry) => (
            <PostReply
              postUri={entry.uri}
              postCid={postCid}
              postClique={postClique}
              authManager={authManager}
            />
          )}
        />
        {replyEntries.length > 0 && (
          <>
            <h4 style={{ padding: "0 1rem" }}>Replies</h4>
            <Feed entries={replyEntries} showPrivatePermalink={false} />
          </>
        )}
      </>
    );
  },
});
