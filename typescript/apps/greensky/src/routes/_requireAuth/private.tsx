import { createFileRoute, Link } from "@tanstack/react-router";
import {
  getPrivatePosts,
  getPostVisibility,
  getProfile,
  getProfiles,
} from "../../habitatApi";
import { type FeedEntry, Feed } from "../../Feed";
import { NavBar } from "../../components/NavBar";

export const Route = createFileRoute("/_requireAuth/private")({
  async loader({ context }) {
    const privatePosts = await getPrivatePosts(context.authManager);

    const entries = await Promise.all(
      privatePosts
        .filter((p) => !p.value.reply)
        .map(async (post): Promise<FeedEntry> => {
          const did = post.uri.split("/")[2] ?? "";
          const [author, grantees] = await Promise.all([
            getProfile(context.authManager, did),
            getProfiles(
              context.authManager,
              (post.resolvedClique ?? []).slice(0, 5),
            ),
          ]);
          return {
            uri: post.uri,
            cid: post.cid,
            clique: post.clique,
            text: post.value.text,
            createdAt: post.value.createdAt,
            kind: getPostVisibility(post, did),
            author,
            grantees: grantees.length > 0 ? grantees : undefined,
          };
        }),
    );

    return entries.filter((e) => e.kind !== "public");
  },
  component() {
    const entries = Route.useLoaderData();
    const { authManager, myProfile, isOnboarded } = Route.useRouteContext();

    return (
      <>
        <NavBar
          left={
            <>
              <li>
                <Link to="/" className="hover:underline">
                  ← greensky
                </Link>
              </li>
              <li className="text-foreground">
                <h3>private feed</h3>
              </li>
            </>
          }
          authManager={authManager}
          myProfile={myProfile}
          isOnboarded={isOnboarded}
        />
        {entries.length === 0 ? (
          <p className="text-sm text-muted-foreground mt-8">
            No private posts yet.
          </p>
        ) : (
          <Feed entries={entries} authManager={authManager} />
        )}
      </>
    );
  },
});
