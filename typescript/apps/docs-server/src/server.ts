import { Hono } from "hono";
import type {
  NetworkHabitatDocsCreateDoc,
  NetworkHabitatDocsUpdateDoc,
  NetworkHabitatDocsListDocs,
  NetworkHabitatDocsListComments,
} from "api";
import type { DerivedConfig } from "./config";
import type { DocCrdtStore } from "./docCrdtStore";
import type { DocMetadataStore } from "./docMetadataStore";
import type { OrgDirectory } from "./orgDirectory";
import type { DocCommentStore } from "./docCommentStore";
import type { PearClient } from "./pearClient";
import {
  ForbiddenError,
  ServiceAuthError,
  ServiceAuthVerifier,
} from "./serviceAuth";

export function createApp(
  config: DerivedConfig,
  pear: PearClient,
  docs: DocCrdtStore,
  meta: DocMetadataStore,
  orgs: OrgDirectory,
  comments: DocCommentStore,
  verifier: ServiceAuthVerifier = new ServiceAuthVerifier(config),
): Hono {
  const app = new Hono();

  // orgFor resolves the org the caller belongs to; every doc operation happens
  // within the caller's org. Callers outside any sap-managed org are rejected.
  function orgFor(caller: string): string {
    const org = orgs.orgForUser(caller);
    if (!org) {
      throw new ForbiddenError("caller does not belong to a known org");
    }
    return org;
  }

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
  // off sap's OAuth flow for the org named in the `handle` form field: sap
  // starts the authorization-code flow and returns the URL to redirect the
  // admin to. Once they approve, sap tracks the org's session and can proxy
  // this server's pear calls (and the crawler's) as that org.
  app.get("/org/login", async (c) => {
    const handle = c.req.query("handle");
    if (!handle) {
      return c.text("missing handle", 400);
    }
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
      await docs.createDoc(caller, orgFor(caller));
    return c.json(output);
  });

  app.post("/xrpc/network.habitat.docs.updateDoc", async (c) => {
    const caller = await authorize(
      c.req.header("Authorization"),
      "network.habitat.docs.updateDoc",
      verifier,
    );
    const org = orgFor(caller);
    const input =
      (await c.req.json()) as NetworkHabitatDocsUpdateDoc.InputSchema;
    const allowed = await pear.check(
      org,
      caller,
      "writer",
      pear.spaceUri(input.docId, org),
    );
    if (!allowed) {
      throw new ForbiddenError("caller cannot write to this doc");
    }
    const output: NetworkHabitatDocsUpdateDoc.OutputSchema =
      await docs.applyUpdate(input.docId, input.update, caller, org);
    return c.json(output);
  });

  // listDocs returns only the docs the caller may read. Permissions are
  // resolved on demand: relationship.listObjects yields the doc spaces the
  // caller holds the reader role on, and the metadata store supplies titles for
  // them.
  app.get("/xrpc/network.habitat.docs.listDocs", async (c) => {
    const caller = await authorize(
      c.req.header("Authorization"),
      "network.habitat.docs.listDocs",
      verifier,
    );
    const spaces = await pear.listReadableSpaces(orgFor(caller), caller);
    const output: NetworkHabitatDocsListDocs.OutputSchema = {
      docs: meta.docsBySpaceUris(spaces).map((d) => ({
        ...d,
        // Owner DID is the 3rd URI segment of ats://<org>/<type>/<skey>.
        commentSpace: pear.commentSpaceUri(d.docId, d.uri.split("/")[2]),
      })),
    };
    return c.json(output);
  });

  // listComments returns a doc's comment threads. Authorization is against the
  // *comment* space (not the doc): comment-only members can read comments
  // without being doc members. The org proxy holds reader on the space, so the
  // check runs as the org on the caller's behalf.
  //
  // If the check fails, we try to backfill the comment space (for docs created
  // before the comment-space feature was added) and retry. This makes old docs
  // automatically gain comment support on first access.
  app.get("/xrpc/network.habitat.docs.listComments", async (c) => {
    const caller = await authorize(
      c.req.header("Authorization"),
      "network.habitat.docs.listComments",
      verifier,
    );
    const org = orgFor(caller);
    const docId = c.req.query("docId");
    console.log("[listComments] called", { caller, org, docId });
    if (!docId) {
      return c.json({ error: "InvalidRequest", message: "missing docId" }, 400);
    }
    const docSpace = pear.spaceUri(docId, org);
    const commentSpace = pear.commentSpaceUri(docId, org);
    console.log("[listComments] spaces", { docSpace, commentSpace });
    let allowed = await pear.check(org, caller, "reader", commentSpace);
    console.log("[listComments] check result", { allowed });
    if (!allowed) {
      // Backfill: create comment space + userset tuples for pre-feature docs.
      console.log("[listComments] backfilling comment space");
      await pear.ensureCommentSpace(org, docSpace, docId);
      allowed = await pear.check(org, caller, "reader", commentSpace);
      console.log("[listComments] backfill check result", { allowed });
    }
    if (!allowed) {
      throw new ForbiddenError("caller cannot read this doc's comments");
    }
    const threads = comments.threadsForDoc(docSpace);
    console.log("[listComments] threadsForDoc result", { docSpace, count: threads.length, threads: JSON.stringify(threads) });
    const output: NetworkHabitatDocsListComments.OutputSchema = {
      comments: threads,
    };
    return c.json(output);
  });

  app.onError((err, c) => {
    if (err instanceof ServiceAuthError) {
      return c.json({ error: "AuthRequired", message: err.message }, 401);
    }
    if (err instanceof ForbiddenError) {
      return c.json({ error: "Forbidden", message: err.message }, 403);
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
