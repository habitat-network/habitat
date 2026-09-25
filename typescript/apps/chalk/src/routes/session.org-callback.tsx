import { env } from "cloudflare:workers";
import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { z } from "zod";
import {
  consumeLoginNonce,
  fetchOrgName,
  requireSession,
} from "@/server/functions.server";
import { switchOrg } from "@/server/functions";
import { getDb, upsertConnectedOrg } from "@/db";
import { SapClient, redeemLogin } from "@/server/sapClient";
import { Button } from "internal/components/ui";
import { ensureValidDid } from "@atproto/syntax";

// connectOrgFn verifies the connection actually works (a member who wasn't
// really an admin never reaches here — pear's HandleOpensocial already
// checked that before completing PDS login) by reading the org's own
// profile, records the connection, and returns the name to show. The name is
// null if that read fails, so the route can render a plain error state; a
// bad or reused code throws instead.
//
// The org DID comes from sap (redeeming the one-time code on the callback
// URL), never from the caller, and the flow must have been started from
// this same browser (consumeLoginNonce) — otherwise anyone could record any
// org as connected without completing its admin sign-in.
const connectOrgFn = createServerFn({ method: "POST" })
  .validator((input: { code: string }) => input)
  .handler(
    async ({ data }): Promise<{ orgDid: string; orgName: string | null }> => {
      const { did } = await requireSession();
      const { did: orgDid, state } = await redeemLogin(env, data.code);
      await consumeLoginNonce(state);
      ensureValidDid(orgDid);
      const client = new SapClient(env, did);
      const orgName = await fetchOrgName(client, orgDid);
      if (orgName === null) return { orgDid, orgName: null };
      await upsertConnectedOrg(getDb(env), {
        memberDid: did,
        orgDid,
        orgName,
      });
      return { orgDid, orgName };
    },
  );

export const Route = createFileRoute("/session/org-callback")({
  validateSearch: z.object({
    code: z.string().optional(),
  }),
  loaderDeps: ({ search }) => ({ code: search.code }),
  loader: async ({ deps }) => {
    if (!deps.code) return { orgDid: undefined, result: null };
    // A bad or already-redeemed code throws; show the same plain error
    // state as a failed connection rather than an error boundary.
    try {
      const { orgDid, orgName } = await connectOrgFn({
        data: { code: deps.code },
      });
      return { orgDid, result: orgName === null ? null : { orgName } };
    } catch (err) {
      console.error("[session.org-callback] connect", err);
      return { orgDid: "", result: null };
    }
  },
  component() {
    const { orgDid, result } = Route.useLoaderData();
    const navigate = Route.useNavigate();

    if (orgDid === undefined) {
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
            await switchOrg({ data: { orgDid } });
            navigate({ to: "/" });
          }}
        >
          Go home
        </Button>
      </div>
    );
  },
});
