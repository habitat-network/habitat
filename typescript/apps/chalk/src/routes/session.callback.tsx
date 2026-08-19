import { createFileRoute, redirect } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { useAppSession } from "@/server/session";

// beforeLoad runs in both the client and server environments (e.g. on
// client-side navigation), so it can't call useAppSession()/useSession()
// directly — that's a server-only API (@tanstack/react-start/server) and
// the build's import-protection plugin rejects pulling it into the client
// bundle. Wrap the session write in a server function instead; only its
// RPC stub reaches the client.
const setSessionDidFn = createServerFn({ method: "POST" })
  .validator((input: { did: string }) => input)
  .handler(async ({ data }) => {
    const session = await useAppSession();
    await session.update({ did: data.did });
  });

// sap redirects the browser here (as this route's URL is what chalk told
// sap's /org/add to use as return_to) once the PDS OAuth handshake
// completes, with the resolved member DID as a query param.
export const Route = createFileRoute("/session/callback")({
  beforeLoad: async ({ search }) => {
    const did = (search as Record<string, string | undefined>).did;
    if (!did) {
      throw redirect({ to: "/login" });
    }
    await setSessionDidFn({ data: { did } });
    throw redirect({ to: "/" });
  },
});
