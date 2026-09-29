import type { AuthManager } from "internal";
import { resolveDidService } from "internal";
import { type Agent } from "@atproto/lex";

// pearAgent sends xrpc calls straight to a habitat instance's own
// management-plane API (network.habitat.*, community.opensocial.*) — this
// app's own instance by default, or an org's own habitat instance when `aud`
// names one — rather than through the caller's own OAuth session. That
// session's requests go wherever the caller's identity resolves its PDS to —
// for a spaces-alpha account talking directly to its own PDS (bypassing
// pear's proxy), that's a third-party server with no idea what these
// habitat-only lexicons are.
//
// Each request mints a fresh com.atproto.server.getServiceAuth token off the
// caller's own session and presents it to the resolved service directly as a
// bearer token — the same service-auth mechanism pear already validates
// (internal/authn.AtprotoServiceAuthMethod) and already mints server-side
// when forwarding an Atproto-Proxy header (internal/forwarding.ServiceProxy),
// just requested explicitly instead of relying on that header, which only
// works when honored by whatever server the caller's session happens to be
// talking to. Built by hand rather than through xrpc(), since a "did#serviceId"
// audience — required to target a specific declared service, exactly what
// ServiceProxy sends server-side — fails xrpc()'s strict bare-DID-only "did"
// param validator.
export function pearAgent(
  authManager: AuthManager,
  aud: string = `did:web:${import.meta.env.VITE_HABITAT_DOMAIN}#habitat`,
): Agent {
  return {
    fetchHandler: async (path, init) => {
      const nsid = path.replace(/^\/xrpc\//, "").split("?")[0];
      const host = await resolveDidService(aud);
      const params = new URLSearchParams({ aud, lxm: nsid });
      const authRes = await authManager.fetch(
        `/xrpc/com.atproto.server.getServiceAuth?${params}`,
      );
      const { token } = await authRes.json();
      const headers = new Headers(init.headers);
      headers.set("Authorization", `Bearer ${token}`);
      return fetch(`${host}${path}`, { ...init, headers });
    },
  };
}
