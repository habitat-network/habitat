import { createFileRoute, Link } from "@tanstack/react-router";
import {
  type DidGranteePermission,
  getPrivatePost,
  getProfile,
  getProfiles,
  getPostVisibility,
} from "../../habitatApi";
import { type FeedEntry, Feed } from "../../Feed";
import { NavBar } from "../../components/NavBar";

export const Route = createFileRoute("/_requireAuth/$handle/p/$rkey")({
  async loader({ context, params }) {
    const [post, profile] = await Promise.all([
      getPrivatePost(context.authManager, params.handle, params.rkey),
      getProfile(context.authManager, params.handle),
    ]);

    if (!post) return [];

    const authorDid = post.uri.split("/")[2] ?? "";
    const granteeDids = (post.permissions ?? [])
      .filter(
        (p): p is DidGranteePermission =>
          p.$type === "network.habitat.grantee#didGrantee",
      )
      .slice(0, 5)
      .map((p) => p.did);
    const grantees = await getProfiles(context.authManager, granteeDids);

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

    return [entry];
  },
  component() {
    const { handle } = Route.useParams();
    const entries = Route.useLoaderData();
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
        <Feed entries={entries} showPrivatePermalink={false} />
      </>
    );
  },
});
