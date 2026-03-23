import { CollectionCard } from "@/components/CollectionCard";
import { createFileRoute, Link } from "@tanstack/react-router";
import { query } from "internal";

export const Route = createFileRoute("/_requireAuth/collections/")({
  component: CollectionsGrid,
  async loader({ context }) {
    const { authManager } = context;
    const collectionsData = await query(
      "network.habitat.repo.listCollections",
      {},
      { authManager },
    );

    const collections = collectionsData.collections;
    // Collect unique DID grantees across all collections
    const granteeDids = [
      ...new Set(
        collections.flatMap((c) =>
          c.grantees
            ? c.grantees
              .filter((g) => g.$type === "network.habitat.grantee#didGrantee")
              .map((g) => (g as { did: string }).did)
            : [],
        ),
      ),
    ];

    // Fetch Bluesky profiles for all DID grantees
    const profilesByDid: Record<string, { avatar?: string; handle: string }> =
      {};
    if (granteeDids.length > 0) {
      const headers = new Headers();
      const params = new URLSearchParams();
      for (const did of granteeDids) params.append("actors", did);
      const resp = await authManager.fetch(
        `/xrpc/app.bsky.actor.getProfiles?${params.toString()}`,
        "GET",
        null,
        headers,
      );
      if (resp.ok) {
        const profileData: {
          profiles: { did: string; handle: string; avatar?: string }[];
        } = await resp.json();
        for (const p of profileData.profiles) {
          profilesByDid[p.did] = { avatar: p.avatar, handle: p.handle };
        }
      }
    }

    return { collections, profilesByDid };
  },
});

function CollectionsGrid() {
  const { collections, profilesByDid } = Route.useLoaderData()!;
  const { authManager } = Route.useRouteContext();

  return (
    <>
      <div className="grid !grid-cols-1 sm:!grid-cols-2 lg:!grid-cols-3 xl:!grid-cols-4">
        {collections.map((collection) => {
          const didGrantees = collection.grantees
            ? (collection.grantees.filter(
              (g) => g.$type === "network.habitat.grantee#didGrantee",
            ) as { did: string }[])
            : [];
          const avatars = didGrantees.map((grantee) => {
            const did = grantee.did;
            return {
              did: did,
              avatar: profilesByDid[did].avatar,
              handle: profilesByDid[did].handle,
            };
          });

          return (
            <Link
              to="/collections/$collection"
              key={collection.nsid}
              params={{ collection: collection.nsid }}
            >
              <CollectionCard
                authManager={authManager}
                collection={collection}
              ></CollectionCard>
            </Link>
          );
        })}
      </div>
    </>
  );
}
