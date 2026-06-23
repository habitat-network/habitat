import { Hono } from "hono";
import type {
  NetworkHabitatDocCreateDoc,
  NetworkHabitatDocUpdateDoc,
} from "api";
import type { DerivedConfig } from "./config";
import type { OrgClient } from "./orgClient";
import type { DocStore } from "./docStore";
import { ServiceAuthError, ServiceAuthVerifier } from "./serviceAuth";

export function createApp(
  config: DerivedConfig,
  org: OrgClient,
  docs: DocStore,
): Hono {
  const app = new Hono();
  const verifier = new ServiceAuthVerifier(config, org);

  // Pending OAuth flows: state -> PKCE verifier. In-memory is fine; the
  // bootstrap is a one-time interactive step.
  const pendingAuth = new Map<string, string>();

  // did:web document. pear resolves did:web:<domain> here and reads the #docs
  // service endpoint to forward network.habitat.doc.* calls to this server.
  app.get("/.well-known/did.json", (c) =>
    c.json({
      "@context": ["https://www.w3.org/ns/did/v1"],
      id: config.did,
      service: [
        {
          id: `#${config.serviceId}`,
          type: "HabitatDocsServer",
          serviceEndpoint: `https://${config.domain}`,
        },
      ],
    }),
  );

  // Confidential OAuth client metadata, fetched by pear during the org auth flow.
  app.get("/client-metadata.json", (c) => c.json(org.clientMetadata()));

  // One-time org credential bootstrap: an org admin opens this to authorize the
  // docs server, then is redirected back to /oauth-callback.
  app.get("/oauth/login", async (c) => {
    const { url, state, verifier } = await org.beginAuth();
    pendingAuth.set(state, verifier);
    return c.redirect(url);
  });

  app.get("/oauth-callback", async (c) => {
    const url = new URL(c.req.url);
    const state = url.searchParams.get("state");
    if (!state || !pendingAuth.has(state)) {
      return c.text("unknown or expired auth state", 400);
    }
    const pkceVerifier = pendingAuth.get(state)!;
    pendingAuth.delete(state);
    try {
      await org.completeAuth(url, state, pkceVerifier);
    } catch (err) {
      return c.text(`failed to complete authorization: ${String(err)}`, 502);
    }
    return c.text("Docs server authorized. You can close this tab.");
  });

  app.post("/xrpc/network.habitat.doc.createDoc", async (c) => {
    const caller = await authorize(
      c.req.header("Authorization"),
      "network.habitat.doc.createDoc",
      verifier,
    );
    const input =
      (await c.req.json()) as NetworkHabitatDocCreateDoc.InputSchema;
    const output: NetworkHabitatDocCreateDoc.OutputSchema =
      await docs.createDoc(input.name, caller);
    return c.json(output);
  });

  app.post("/xrpc/network.habitat.doc.updateDoc", async (c) => {
    const caller = await authorize(
      c.req.header("Authorization"),
      "network.habitat.doc.updateDoc",
      verifier,
    );
    const input =
      (await c.req.json()) as NetworkHabitatDocUpdateDoc.InputSchema;
    const output: NetworkHabitatDocUpdateDoc.OutputSchema =
      await docs.applyUpdate(input.docId, input.update, caller);
    return c.json(output);
  });

  app.onError((err, c) => {
    if (err instanceof ServiceAuthError) {
      return c.json({ error: "AuthRequired", message: err.message }, 401);
    }
    return c.json({ error: "InternalServerError", message: String(err) }, 500);
  });

  return app;
}

async function authorize(
  authHeader: string | undefined,
  lxm: string,
  verifier: ServiceAuthVerifier,
): Promise<string> {
  const jwt = authHeader?.replace(/^Bearer /, "");
  if (!jwt) {
    throw new ServiceAuthError("missing service auth token");
  }
  return verifier.verify(jwt, lxm);
}
