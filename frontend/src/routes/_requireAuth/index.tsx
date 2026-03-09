import { createFileRoute, Link } from "@tanstack/react-router";
import { DidResolver } from "@atproto/identity";
import { OnboardComponent, habitatServers } from "../onboard";
import { listCollections } from "internal";
import { CollectionMetadata } from "api/types/network/habitat/repo/listCollections";
import { CollectionCard } from "@/components/CollectionCard"

export const Route = createFileRoute("/_requireAuth/")({
  async loader({ context }) {
    const { authManager } = context;

    const did = authManager.getAuthInfo()!.did;
    const resolver = new DidResolver({});
    const didDoc = await resolver.resolve(did);

    const serviceKey = import.meta.env.DEV ? "habitat_local" : "habitat";
    const hasHabitat = didDoc?.service?.some(
      (s) => s.id === `#${serviceKey}` && s.type === "HabitatServer",
    );
    const handle = didDoc?.alsoKnownAs?.[0]?.replace(/^at:\/\//, "");

    // List collections for manage your data preview
    const data = await listCollections(authManager, did)
    const collections = data.collections.slice(0, 3);  // Just show the first three in the preview

    // Collect unique DID grantees across all collections
    const granteeDids = [
      ...new Set(
        collections.flatMap((c) =>
          c.grantees ?
            c.grantees
              .filter((g) => g.$type === "network.habitat.grantee#didGrantee")
              .map((g) => (g as { did: string }).did)
            : []
        ),
      ),
    ];

    // Fetch Bluesky profiles for all DID grantees
    const profilesByDid: Record<string, { avatar?: string; handle: string }> = {};
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
        const profileData: { profiles: { did: string; handle: string; avatar?: string }[] } =
          await resp.json();
        for (const p of profileData.profiles) {
          profilesByDid[p.did] = { avatar: p.avatar, handle: p.handle };
        }
      }
    }

    return { hasHabitat, handle, collections, profilesByDid };
  },
  pendingComponent: () => <p>Loading...</p>,
  component() {
    return <AuthenticatedHome />;
  },
});

function RecentlyUsed() {
  return (
    <h4> Recently used apps </h4>
  )
}

interface ManageDataPreviewProps {
  collections: CollectionMetadata[];
  profilesByDid: Record<string, { avatar?: string; handle: string }>;
}

function ManageDataPreview({ collections, profilesByDid }: ManageDataPreviewProps) {
  return (
    <>
      <Link to="/explore">
        <h4> Manage your data </h4>
      </Link>

      <div className="grid">
        {collections.map((collection) => {
          const didGrantees = collection.grantees ? collection.grantees.filter(
            (g) => g.$type === "network.habitat.grantee#didGrantee",
          ) as { did: string }[] : [];
          const avatars = didGrantees.map((grantee) => {
            const did = grantee.did;
            return {
              did: did,
              avatar: profilesByDid[did].avatar,
              handle: profilesByDid[did].handle,
            }
          })

          const formatted = { ...collection, grantees: avatars }
          return (
            <Link to="/collections/$collection" key={formatted.nsid} params={{ collection: formatted.nsid }}>
              <CollectionCard collection={formatted}></CollectionCard>
            </Link>
          );
        })}
      </div>
    </>
  )
}

function AuthenticatedHome() {
  const { hasHabitat, handle, collections, profilesByDid } = Route.useLoaderData()!;

  if (!hasHabitat) {
    return import.meta.env.DEV ? (
      <OnboardComponent
        serviceKey="habitat_local"
        title="Onboard (Local)"
        defaultServer="https://pear.taile529e.ts.net"
        handle={handle}
      />
    ) : (
      <OnboardComponent serverOptions={habitatServers} handle={handle} />
    );
  }

  return (
    <>
      <h1>Welcome to Habitat!</h1>
      <RecentlyUsed></RecentlyUsed>
      <ManageDataPreview collections={collections} profilesByDid={profilesByDid} />
      <br />
      <Link to="/devtools">Devtools</Link>
    </>
  );
}
