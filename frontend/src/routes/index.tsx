import { createFileRoute, Link } from "@tanstack/react-router";
import { DidResolver } from "@atproto/identity";
import { OnboardComponent, habitatServers } from "./onboard";
import { Card, CardDescription } from "internal";

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
    const response = await context.authManager.fetch(
      `/xrpc/network.habitat.repo.listCollections?subject=${did}`,
    );
    const data: {
      collections: any,
    } = await response?.json();

    // For each collection, get metadata


    return { hasHabitat, handle, collections: data.collections };
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
    <p>Fill me in</p>
  )
}

interface ManageDataPreviewProps {
  collections: any[]
}


function ManageDataPreview({ collections }: ManageDataPreviewProps) {
  console.log(collections)
  return (
    <ul>
      {collections.map((collection) => (
        <Card>
          <CardDescription>{collection.nsid}</CardDescription>
        </Card>
      ))}
    </ul>
  )
}

function AuthenticatedHome() {
  const { hasHabitat, handle, collections } = Route.useLoaderData()!;

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
      <ManageDataPreview collections={collections} />
      <Link to="/explore">Manage your data</Link>
      <br />
      <Link to="/devtools">Devtools</Link>
    </>
  );
}
