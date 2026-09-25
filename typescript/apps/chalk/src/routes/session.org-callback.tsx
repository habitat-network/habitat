import { env } from "cloudflare:workers";
import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { z } from "zod";
import {
  completeOrgConnect,
  requireSession,
  setCurrentOrg,
} from "@/server/functions.server";
import { getDb } from "@/db";
import { Button } from "internal/components/ui";

// connectOrgFn redeems sap's single-use code for the org DID, verifies and
// records the connection, and returns the org to show (see
// completeOrgConnect). It takes the code, not an org DID: this server
// function is callable directly by anyone, so the org DID must come from
// sap. Returns null on failure instead of throwing, so the route can render
// a plain error state.
const connectOrgFn = createServerFn({ method: "POST" })
  .validator((input: { code: string }) => input)
  .handler(
    async ({ data }): Promise<{ orgDid: string; orgName: string } | null> =>
      completeOrgConnect(env, getDb(env), data.code),
  );

const setCurrentOrgFn = createServerFn({ method: "POST" })
  .validator((input: { orgDid: string }) => input)
  .handler(async ({ data }) => {
    await requireSession();
    await setCurrentOrg(data.orgDid);
  });

export const Route = createFileRoute("/session/org-callback")({
  validateSearch: z.object({
    code: z.string().optional(),
  }),
  loaderDeps: ({ search }) => ({ code: search.code }),
  // The code is single-use, so never re-run the loader for the same URL —
  // a second exchange would fail and flip a successful connect to an error.
  staleTime: Infinity,
  loader: async ({ deps }) => {
    if (!deps.code) return { missing: true, result: null };
    const result = await connectOrgFn({ data: { code: deps.code } });
    return { missing: false, result };
  },
  component() {
    const { missing, result } = Route.useLoaderData();
    const navigate = Route.useNavigate();

    if (missing) {
      return <p>Missing org — please try connecting again from /orgs.</p>;
    }
    if (!result) {
      return (
        <p>
          Couldn't connect this org — you may not be an admin of it, or the
          connection failed. Please try again from /orgs.
        </p>
      );
    }
    return (
      <div className="flex flex-col items-center gap-4 py-32">
        <p>Successfully approved Chalk with {result.orgName}</p>
        <Button
          onClick={async () => {
            await setCurrentOrgFn({ data: { orgDid: result.orgDid } });
            navigate({ to: "/" });
          }}
        >
          Go home
        </Button>
      </div>
    );
  },
});
