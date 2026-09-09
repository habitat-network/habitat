import { createFileRoute } from "@tanstack/react-router";
import { agentFor } from "internal";
import { z } from "zod";
import {
  xrpc,
  type AtIdentifierString,
  type NsidString,
  type RecordKeyString,
} from "@atproto/lex";
import { network } from "api";

export const Route = createFileRoute("/_requireAuth/pear-test/view")({
  validateSearch: z.object({
    did: z.string(),
    rkey: z.string(),
  }),
  loaderDeps: ({ search }) => search,
  async loader({ deps: { did, rkey }, context }) {
    const json = await xrpc(
      agentFor(context.authManager),
      network.habitat.repo.getRecord.main,
      {
        params: {
          collection: "network.habitat.test" as NsidString,
          repo: did as AtIdentifierString,
          rkey: rkey as RecordKeyString,
        },
      },
    );
    return JSON.stringify(json.body);
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
