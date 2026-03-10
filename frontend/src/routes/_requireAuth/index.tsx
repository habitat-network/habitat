import { createFileRoute, Link } from "@tanstack/react-router";
import { query } from "internal";
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
    )

    // Fetch logo URIs from each app's client metadata (clientID is a URI to the metadata doc)
    const logosByClientID: Record<string, string | undefined> = {};
    await Promise.all(
      appData.apps.map(async (app) => {
        console.log("fetching", app.clientID)
        try {
          const resp = await fetch(app.clientID);
          if (resp.ok) {
            const data = await resp.json();
            console.log("resp", data)
            const meta: { logo_uri?: string } = data;
            logosByClientID[app.clientID] = meta.logo_uri;
          }
          console.log("here", logosByClientID)
        } catch {
          // ignore
          console.log("caught")
        }
      }),
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
      headers.append("at-proxy", "did:web:api.bsky.app#bsky_appview");
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

    return { collections, apps: appData.apps, profilesByDid, logosByClientID };
  },
  pendingComponent: () => <p>Loading...</p>,
  component() {
    return <AuthenticatedHome />;
  },
});

interface RecentlyUsedProps {
  apps: App[];
  logosByClientID: Record<string, string | undefined>;
}

function RecentlyUsed({ apps, logosByClientID }: RecentlyUsedProps) {
  return (
    <>
      <h4> Recently used apps </h4>
      <div>
        {apps.map((app) => {
          const logoUri = logosByClientID[app.clientID];
          return logoUri ? (
            <img key={app.clientID} src={logoUri} alt={app.name} />
          ) : null;
        })}
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
    </>
  );
}

function AuthenticatedHome() {
  const { collections, apps, profilesByDid, logosByClientID } =
    Route.useLoaderData()!;

  // For now, don't require the user to be registered with a habitat service. If they do have one,
  // requests will still be routed there, but allow them to use the centralized one by default.

  return (
    <>
      <h1>Welcome to Habitat!</h1>
      <RecentlyUsed apps={apps} logosByClientID={logosByClientID} />
      <ManageDataPreview
        collections={collections}
        profilesByDid={profilesByDid}
      />
      <br />
      <Link to="/devtools">Devtools</Link>
    </>
  );
}
