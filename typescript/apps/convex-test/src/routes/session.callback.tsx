// typescript/apps/convex-test/src/routes/session.callback.tsx
import { createFileRoute, redirect } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { z } from "zod";
import { useAppSession } from "~/server/session";

const setSessionDidFn = createServerFn({ method: "POST" })
  .validator((input: { did: string }) => input)
  .handler(async ({ data }) => {
    const session = await useAppSession();
    await session.update({ did: data.did });
  });

export const Route = createFileRoute("/session/callback")({
  validateSearch: z.object({ did: z.string().optional() }),
  beforeLoad: async ({ search }) => {
    if (!search.did) throw redirect({ to: "/login" });
    await setSessionDidFn({ data: { did: search.did } });
    throw redirect({ to: "/" });
  },
});
