import { CollectionCard } from "@/components/CollectionCard";
import { createFileRoute, Link } from "@tanstack/react-router";
import { query, getProfiles } from "internal";

export const Route = createFileRoute("/_requireAuth/collections/")({
  component: CollectionsGrid,
  async loader({ context }) {
    const { authManager } = context;
    const collectionsData = await query(
      "network.habitat.repo.describeRepo",
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
      const profiles = await getProfiles(granteeDids);
      for (const p of profiles) {
        profilesByDid[p.did] = { avatar: p.avatar, handle: p.handle ?? "" };
      }
    }

    return { collections, profilesByDid };
  },
});

function CollectionsGrid() {
  const { collections } = Route.useLoaderData()!;
  const { authManager } = Route.useRouteContext();

  return (
    <>
      <div className="grid !grid-cols-1 sm:!grid-cols-2 lg:!grid-cols-3 xl:!grid-cols-4">
        {collections.map((collection) => {
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
