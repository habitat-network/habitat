import { createFileRoute } from "@tanstack/react-router";
import { query } from "internal";

export const Route = createFileRoute("/_requireAuth/pear-test/view")({
  validateSearch(search) {
    return {
      did: search.did as string,
      rkey: search.rkey as string,
    };
  },
  loaderDeps: ({ search }) => search,
  async loader({ deps: { did, rkey }, context }) {
    const json = await query(
      "network.habitat.repo.getRecord",
      { repo: did, rkey, collection: "network.habitat.test" },
      { authManager: context.authManager },
    );
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
