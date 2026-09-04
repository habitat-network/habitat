import { env } from "cloudflare:workers";
import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { z } from "zod";
import {
  fetchOrgName,
  requireSession,
  setCurrentOrg,
} from "@/server/functions.server";
import { getDb, upsertConnectedOrg } from "@/db";
import { SapClient } from "@/server/sapClient";
import { Button } from "internal/components/ui";

// connectOrgFn verifies the connection actually works (a member who wasn't
// really an admin never reaches here — pear's HandleOpensocial already
// checked that before completing PDS login) by reading the org's own
// profile, records the connection, and returns the name to show. Returns
// null on failure instead of throwing, so the route can render a plain
// error state.
const connectOrgFn = createServerFn({ method: "POST" })
  .validator((input: { orgDid: string }) => input)
  .handler(async ({ data }): Promise<{ orgName: string } | null> => {
    const { did } = await requireSession();
    const client = new SapClient(env, did);
    const orgName = await fetchOrgName(client, data.orgDid);
    if (orgName === null) return null;
    await upsertConnectedOrg(getDb(env), {
      memberDid: did,
      orgDid: data.orgDid,
      orgName,
    });
    return { orgName };
  });

const setCurrentOrgFn = createServerFn({ method: "POST" })
  .validator((input: { orgDid: string }) => input)
  .handler(async ({ data }) => {
    await requireSession();
    await setCurrentOrg(data.orgDid);
  });

export const Route = createFileRoute("/session/org-callback")({
  validateSearch: z.object({
    did: z.string().optional(),
  }),
  loaderDeps: ({ search }) => ({ did: search.did }),
  loader: async ({ deps }) => {
    if (!deps.did) return { orgDid: undefined, result: null };
    const result = await connectOrgFn({ data: { orgDid: deps.did } });
    return { orgDid: deps.did, result };
  },
  component() {
    const { orgDid, result } = Route.useLoaderData();
    const navigate = Route.useNavigate();

    if (!orgDid) {
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
            await setCurrentOrgFn({ data: { orgDid } });
            navigate({ to: "/" });
          }}
        >
          Go home
        </Button>
      </div>
    );
  },
});
