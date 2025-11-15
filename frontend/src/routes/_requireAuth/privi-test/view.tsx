import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_requireAuth/privi-test/view")({
  validateSearch(search) {
    return {
      did: search.did as string,
      rkey: search.rkey as string,
    };
  },
  loaderDeps: ({ search }) => search,
  async loader({ deps: { did, rkey }, context }) {
    const params = new URLSearchParams();
    params.set("repo", did || "did:plc:v3amhno5wvyfams6aioqqj66");
    params.set("rkey", rkey || "testRecord");
    params.set("collection", "com.habitat.test");
    const response = await context.authManager?.fetch(
      `/xrpc/com.habitat.getRecord?${params.toString()}`,
    );
    const json = await response?.json();
    return json.foo;
  },
  component() {
    const message = Route.useLoaderData();
    return (
      <div className="border rounded p-4">
        <p>{message}</p>
      </div>
    );
  },
});
