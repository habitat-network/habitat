import { createFileRoute, Link } from "@tanstack/react-router";
import { query } from "internal";
import { Card, CardDescription, SearchBar } from "internal/components/ui";
import { CollectionMetadata } from "api/types/network/habitat/repo/listCollections";
import { CollectionCard } from "@/components/CollectionCard";
import { App } from "api/types/network/habitat/listConnectedApps";

export const Route = createFileRoute("/_requireAuth/")({
  async loader({ context }) {
    const { authManager } = context;
    const appData = await query(
      "network.habitat.listConnectedApps",
      {},
      { authManager },
    );

    // List collections for manage your data preview
    const data = await query(
      "network.habitat.repo.listCollections",
      {},
      { authManager },
    );
    const collections = data.collections.slice(0, 3); // Just show the first three in the preview

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

    const apps = appData.apps.filter(
      (app) => app.clientUri !== `https://${__DOMAIN__}`,
    );
    return { collections, apps: apps, profilesByDid };
  },
  pendingComponent: () => <p>Loading...</p>,
  component() {
    return <AuthenticatedHome />;
  },
});

interface RecentlyUsedProps {
  apps: App[];
}

function RecentlyUsed({ apps }: RecentlyUsedProps) {
  return (
    <>
      <h4> Recently used apps </h4>
      <div className="flex flex-wrap gap-3">
        {apps.map((app) => (
          <Card key={app.clientID} className="ring-0">
            <Link to={app.clientUri}>
              {app.logoUri ? (
                <img
                  src={app.logoUri}
                  alt={app.name}
                  className="w-12 h-12 rounded-lg object-contain"
                />
              ) : null}
              <CardDescription className="text-xs text-center truncate w-full px-1">
                {app.name}
              </CardDescription>
            </Link>
          </Card>
        ))}
      </div>
    </>
  );
}

interface ManageDataPreviewProps {
  collections: CollectionMetadata[];
  profilesByDid: Record<string, { avatar?: string; handle: string }>;
}

function ManageDataPreview({
  collections,
  profilesByDid,
}: ManageDataPreviewProps) {
  return (
    <>
      <Link to="/explore">
        <h4> Manage your data </h4>
      </Link>

      <div className="grid">
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

          const formatted = { ...collection, grantees: avatars };
          return (
            <Link
              to="/collections/$collection"
              key={formatted.nsid}
              params={{ collection: formatted.nsid }}
            >
              <CollectionCard collection={formatted}></CollectionCard>
            </Link>
          );
        })}
      </div>
      <div className="flex justify-end mt-1">
        <Link to="/collections" className="text-sm !no-underline">See all →</Link>
      </div>
    </>
  );
}

function AuthenticatedHome() {
  const { collections, apps, profilesByDid } = Route.useLoaderData()!;

  // For now, don't require the user to be registered with a habitat service. If they do have one,
  // requests will still be routed there, but allow them to use the centralized one by default.

  return (
    <>
      <h1>Welcome to Habitat!</h1>
      <SearchBar disabled={true} placeholder="Search your data for anything ... coming soon!"></SearchBar>
      <RecentlyUsed apps={apps} />
      <ManageDataPreview
        collections={collections}
        profilesByDid={profilesByDid}
      />
      <br />
      <Link to="/devtools">Devtools</Link>
    </>
  );
}
