import { createFileRoute } from "@tanstack/react-router";
import { query } from "internal";
import { z } from "zod";

export const Route = createFileRoute("/_requireAuth/pear-test/view")({
  validateSearch: z.object({
    did: z.string(),
    rkey: z.string(),
  }),
  loaderDeps: ({ search }) => search,
  async loader({ deps: { did, rkey }, context }) {
    const json = await query(
      "network.habitat.repo.getRecord",
      { repo: did, rkey, collection: "network.habitat.test" },
      { fetcher: context.authManager },
    );
    return JSON.stringify(json);
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
