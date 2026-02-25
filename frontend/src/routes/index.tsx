/// <reference types="vite/client" />
import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { DidResolver } from "@atproto/identity";
import { OnboardComponent, habitatServers } from "./onboard";

export const Route = createFileRoute("/")({
  async beforeLoad({ context }) {
    await context.authManager.maybeExchangeCode(window.location.href);
  },
  component() {
    const { authManager } = Route.useRouteContext();

    if (!authManager.isAuthenticated()) {
      return (
        <>
          <h1>Welcome to Habitat!</h1>
          <p>Please sign in to continue.</p>
        </>
      );
    }

    return <AuthenticatedHome authManager={authManager} />;
  },
});

function AuthenticatedHome({
  authManager,
}: {
  authManager: ReturnType<typeof Route.useRouteContext>["authManager"];
}) {
  const did = authManager.getAuthInfo()!.did;

  const { data: didDoc, isLoading } = useQuery({
    queryKey: ["didDoc", did],
    queryFn: async () => {
      const resolver = new DidResolver({});
      return resolver.resolve(did);
    },
  });

  if (isLoading) return <p>Loading...</p>;

  const serviceKey = import.meta.env.DEV ? "habitat_local" : "habitat";
  const hasHabitat = didDoc?.service?.some(
    (s) => s.id === `#${serviceKey}` && s.type === "HabitatServer",
  );

  // alsoKnownAs entries are formatted as "at://handle.bsky.social"
  const handle = didDoc?.alsoKnownAs?.[0]?.replace(/^at:\/\//, "");

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
      <Link to="/explore">Manage your data</Link>
      <br />
      <Link to="/devtools">Devtools</Link>
    </>
  );
}
