import { Hono } from "hono";
import type {
  NetworkHabitatDocsCreateDoc,
  NetworkHabitatDocsUpdateDoc,
  NetworkHabitatDocsListDocs,
} from "api";
import type { DerivedConfig } from "./config";
import type { DocStore } from "./docStore";
import type { CrawlStore } from "./crawlStore";
import { ServiceAuthError, ServiceAuthVerifier } from "./serviceAuth";

export function createApp(
  config: DerivedConfig,
  docs: DocStore,
  crawl: CrawlStore,
): Hono {
  const app = new Hono();
  const verifier = new ServiceAuthVerifier(config);

  // did:web document. pear resolves did:web:<domain> here and reads the #docs
  // service endpoint to forward network.habitat.docs.* calls to this server.
  // Allow any origin so the docsv2 frontend can resolve it to find the org-login
  // endpoint.
  app.get("/.well-known/did.json", (c) => {
    c.header("Access-Control-Allow-Origin", "*");
    return c.json({
      "@context": ["https://www.w3.org/ns/did/v1"],
      id: config.did,
      service: [
        {
          id: `#${config.serviceId}`,
          type: "HabitatDocsServer",
          serviceEndpoint: `https://${config.domain}`,
        },
      ],
    });
  });

  // Org-credential bootstrap. An org admin (redirected here from docsv2) kicks
  // off sap's OAuth flow for the org: sap starts the authorization-code flow and
  // returns the URL to redirect the admin to. Once they approve, sap tracks the
  // org's session and can proxy this server's pear calls as the org. The org
  // handle comes from the form; it defaults to the configured org, which is the
  // one this server proxies its own calls as.
  app.get("/org/login", async (c) => {
    const handle = c.req.query("handle") || config.orgHandle;
    const res = await fetch(`${config.sapUrl}/org/add`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ handle }),
    });
    if (!res.ok) {
      return c.text(`failed to start org auth: ${await res.text()}`, 502);
    }
    const { redirect_url } = (await res.json()) as { redirect_url: string };
    return c.redirect(redirect_url);
  });

  app.post("/xrpc/network.habitat.docs.createDoc", async (c) => {
    const caller = await authorize(
      c.req.header("Authorization"),
      "network.habitat.docs.createDoc",
      verifier,
    );
    const output: NetworkHabitatDocsCreateDoc.OutputSchema =
      await docs.createDoc(caller);
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

  // listDocs returns only the docs the caller may read. The set is served from
  // the crawl store, which the sap crawler keeps up to date: it records each
  // doc and, via relationship.listSubjects, the members that hold read access.
  app.get("/xrpc/network.habitat.docs.listDocs", async (c) => {
    const caller = await authorize(
      c.req.header("Authorization"),
      "network.habitat.docs.listDocs",
      verifier,
    );
    const output: NetworkHabitatDocsListDocs.OutputSchema = {
      docs: crawl.listDocsForSubject(caller),
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
