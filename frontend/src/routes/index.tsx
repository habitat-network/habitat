import { createFileRoute, Link } from "@tanstack/react-router";
import { DidResolver } from "@atproto/identity";
import { OnboardComponent, habitatServers } from "./onboard";
import { Card, CardTitle, CardDescription, CardFooter, listCollections, UserAvatar } from "internal";
import { CollectionMetadata } from "api/types/network/habitat/repo/listCollections";

export const Route = createFileRoute("/")({
  async beforeLoad({ context }) {
    await context.authManager.maybeExchangeCode();
  },
  async loader({ context }) {
    const { authManager } = context;
    if (!authManager.getAuthInfo()) return null;

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
    const collections = data.collections;

    console.log(JSON.stringify(collections))
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

    return { hasHabitat, handle, collections: data.collections, profilesByDid };
  },
  pendingComponent: () => <p>Loading...</p>,
  component() {
    const { authManager } = Route.useRouteContext();

    if (!authManager.getAuthInfo()) {
      return (
        <>
          <h1>Welcome to Habitat!</h1>
          <p>Please sign in to continue.</p>
        </>
      );
    }

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
  const rows: CollectionMetadata[][] = [];
  for (let i = 0; i < collections.length; i += 3) {
    rows.push(collections.slice(i, i + 3));
  }

  return (
    <>
      <Link to="/explore">
        <h4> Manage your data </h4>
      </Link>

      <div>
        {rows.map((row, i) => (
          <div key={i} className="grid">
            {row.map((collection) => {
              const didGrantees = collection.grantees ? collection.grantees.filter(
                (g) => g.$type === "network.habitat.grantee#didGrantee",
              ) as { did: string }[] : [];
              return (
                <Card key={collection.nsid}>
                  <div className="flex items-center justify-between px-6">
                    <CardTitle>{collection.nsid}</CardTitle>
                    <span className="text-sm text-muted-foreground">{collection.recordCount} {(collection.recordCount > 1) ? "records" : "record"}</span>
                  </div>
                  <CardDescription className="px-6">Last updated: {new Date(collection.lastTouched).toLocaleDateString()}</CardDescription>
                  <CardFooter>
                    <div className="flex gap-1">
                      {didGrantees.map((g) => {
                        const profile = profilesByDid[g.did];
                        return (
                          <UserAvatar
                            key={g.did}
                            src={profile?.avatar}
                            handle={profile?.handle}
                            size="sm"
                          />
                        );
                      })}
                    </div>
                  </CardFooter>
                </Card>
              );
            })}
          </div>
        ))}
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
