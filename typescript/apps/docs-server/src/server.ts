import { Hono } from "hono";
import type {
  NetworkHabitatDocsCreateDoc,
  NetworkHabitatDocsUpdateDoc,
  NetworkHabitatDocsListDocs,
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
  // service endpoint to forward network.habitat.docs.* calls to this server.
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

  app.post("/xrpc/network.habitat.docs.createDoc", async (c) => {
    const caller = await authorize(
      c.req.header("Authorization"),
      "network.habitat.docs.createDoc",
      verifier,
    );
    const input =
      (await c.req.json()) as NetworkHabitatDocsCreateDoc.InputSchema;
    const output: NetworkHabitatDocsCreateDoc.OutputSchema =
      await docs.createDoc(input.name, caller);
    return c.json(output);
  });

  app.post("/xrpc/network.habitat.docs.updateDoc", async (c) => {
    const caller = await authorize(
      c.req.header("Authorization"),
      "network.habitat.docs.updateDoc",
      verifier,
    );
    const input =
      (await c.req.json()) as NetworkHabitatDocsUpdateDoc.InputSchema;
    const output: NetworkHabitatDocsUpdateDoc.OutputSchema =
      await docs.applyUpdate(input.docId, input.update, caller);
    return c.json(output);
  });

  // listDocs returns every doc in the org (no per-caller filtering). The caller
  // is still authenticated via service auth before listing.
  app.get("/xrpc/network.habitat.docs.listDocs", async (c) => {
    await authorize(
      c.req.header("Authorization"),
      "network.habitat.docs.listDocs",
      verifier,
    );
    const output: NetworkHabitatDocsListDocs.OutputSchema = {
      docs: await docs.listDocs(),
    };
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
