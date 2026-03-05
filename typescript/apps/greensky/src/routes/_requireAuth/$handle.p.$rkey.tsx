import { createFileRoute, Link } from "@tanstack/react-router";
import {
  getPrivatePost,
  getPrivatePosts,
  getProfile,
  getProfiles,
  getPostVisibility,
} from "../../habitatApi";
import { type FeedEntry, Feed } from "../../Feed";
import { NavBar } from "../../components/NavBar";

export const Route = createFileRoute("/_requireAuth/$handle/p/$rkey")({
  async loader({ context, params }) {
    const [post, profile, allPosts] = await Promise.all([
      getPrivatePost(context.authManager, params.handle, params.rkey),
      getProfile(context.authManager, params.handle),
      getPrivatePosts(context.authManager),
    ]);

    if (!post)
      return {
        entries: [] as FeedEntry[],
        replyEntries: [] as FeedEntry[],
      };

    const authorDid = post.uri.split("/")[2] ?? "";
    const granteeDids = (post.resolvedClique ?? []).slice(0, 5);

    const replyPosts = allPosts.filter(
      (p) => p.value.reply?.parent?.uri === post.uri,
    );

    const replyAuthorDids = [
      ...new Set(replyPosts.map((r) => r.uri.split("/")[2] ?? "")),
    ];

    const [grantees, replyAuthorProfiles] = await Promise.all([
      getProfiles(context.authManager, granteeDids),
      getProfiles(context.authManager, replyAuthorDids),
    ]);

    const replyAuthorByDid = new Map(
      replyAuthorDids.map((did, i) => [did, replyAuthorProfiles[i]]),
    );

    const entry: FeedEntry = {
      uri: post.uri,
      cid: post.cid,
      clique: post.clique,
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
      const replyAuthorDid = reply.uri.split("/")[2] ?? "";
      const replyAuthor = replyAuthorByDid.get(replyAuthorDid);
      return {
        uri: reply.uri,
        cid: reply.cid,
        clique: reply.clique,
        text: reply.value.text,
        createdAt: reply.value.createdAt,
        kind: getPostVisibility(reply, replyAuthorDid),
        author: replyAuthor
          ? { handle: replyAuthor.handle, avatar: replyAuthor.avatar }
          : undefined,
        replyToHandle: profile.handle,
      };
    });

    return {
      entries: [entry],
      replyEntries: replyEntries,
    };
  },
  component() {
    const { handle } = Route.useParams();
    const { entries, replyEntries } = Route.useLoaderData() as {
      entries: FeedEntry[];
      replyEntries: FeedEntry[];
    };
    const { authManager, myProfile, isOnboarded } = Route.useRouteContext();
    return (
      <>
        <NavBar
          left={
            <>
              <li>
                <Link to="/">← greensky</Link>
              </li>
              <li className="hidden sm:block text-foreground">
                post by @{handle}
              </li>
            </>
          }
          mobileTitle={<span>post by @{handle}</span>}
          authManager={authManager}
          myProfile={myProfile}
          isOnboarded={isOnboarded}
        />
        <Feed
          entries={entries}
          showPrivatePermalink={false}
          authManager={authManager}
        />
        {replyEntries.length > 0 && (
          <>
            <h4 className="w-full max-w-2xl mx-2 my-4">Replies</h4>
            <Feed
              entries={replyEntries}
              showPrivatePermalink={true}
              authManager={authManager}
            />
          </>
        )}
      </>
    );
  },
});
