import { Hono } from "hono";
import type { Context } from "hono";
import type { ContentfulStatusCode } from "hono/utils/http-status";
import { getCookie, setCookie, deleteCookie } from "hono/cookie";
import type {
  NetworkHabitatDocsCreateDoc,
  NetworkHabitatDocsUpdateDoc,
  NetworkHabitatDocsListDocs,
  NetworkHabitatSpaceGetRecord,
} from "api";
import type { DerivedConfig } from "./config";
import type { DocCrdtStore } from "./docCrdtStore";
import type { DocMetadataStore } from "./docMetadataStore";
import type { OrgDirectory } from "./orgDirectory";
import type { PearClient } from "./pearClient";
import type { SessionStore } from "./sessionStore";
import {
  ForbiddenError,
  ServiceAuthError,
  ServiceAuthVerifier,
} from "./serviceAuth";

// Cookie holding the server-session token for the docsv2 frontend.
const SESSION_COOKIE = "docs_session";

const CRDT_COLLECTION = "network.habitat.docs.crdt";
const SELF = "self";

export function createApp(
  config: DerivedConfig,
  pear: PearClient,
  docs: DocCrdtStore,
  meta: DocMetadataStore,
  orgs: OrgDirectory,
  sessions: SessionStore,
): Hono {
  const app = new Hono();
  const verifier = new ServiceAuthVerifier(config);

  // orgFor resolves the org the caller belongs to; every doc operation happens
  // within the caller's org. Callers outside any sap-managed org are rejected.
  function orgFor(caller: string): string {
    const org = orgs.orgForUser(caller);
    if (!org) {
      throw new ForbiddenError("caller does not belong to a known org");
    }
    return org;
  }

  // sessionDid resolves the user DID from the server-session cookie, or
  // undefined if there is no valid session.
  function sessionDid(c: Context): string | undefined {
    const token = getCookie(c, SESSION_COOKIE);
    if (!token) {
      return undefined;
    }
    return sessions.didFor(token);
  }

  // requireSession returns the session user DID or throws — used by endpoints
  // that must act as a specific user (applyUpdate writes that user's CRDT
  // record, and the generic pear proxy authenticates as them).
  function requireSession(c: Context): string {
    const did = sessionDid(c);
    if (!did) {
      throw new ServiceAuthError("no server session");
    }
    return did;
  }

  // resolveCaller authenticates a request as either a server session or a
  // service-auth JWT (pear-signed, forwarded here). The session takes
  // precedence; service auth remains valid for every endpoint except
  // applyUpdate, which needs the user's own credential.
  async function resolveCaller(c: Context, lxm: string): Promise<string> {
    const did = sessionDid(c);
    if (did) {
      return did;
    }
    const jwt = c.req.header("Authorization")?.replace(/^Bearer /, "");
    if (!jwt) {
      throw new ServiceAuthError("missing session and service auth token");
    }
    return verifier.verify(jwt, lxm);
  }

  // CORS: the docsv2 frontend lives on a different origin and authenticates
  // with the server-session cookie, so it needs credentialed CORS scoped to
  // that exact origin. Other origins get no CORS headers.
  app.use("*", async (c, next) => {
    const origin = c.req.header("Origin");
    if (origin === config.appOrigin) {
      c.header("Access-Control-Allow-Origin", origin);
      c.header("Access-Control-Allow-Credentials", "true");
      c.header("Vary", "Origin");
    }
    if (c.req.method === "OPTIONS") {
      c.header("Access-Control-Allow-Methods", "GET, POST, OPTIONS");
      c.header(
        "Access-Control-Allow-Headers",
        "content-type, authorization, atproto-proxy",
      );
      return c.body(null, 204);
    }
    return next();
  });

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

  // User login. The docsv2 login page navigates the browser here (top-level, so
  // no CORS). sap starts the user's OAuth flow and, once they approve, redirects
  // back to /session/callback with a single-use login token.
  app.get("/login", async (c) => {
    const handle = c.req.query("handle");
    if (!handle) {
      return c.text("missing handle", 400);
    }
    const redirectUrl = `https://${config.domain}/session/callback`;
    let authUrl: string;
    try {
      authUrl = await pear.startUserLogin(handle, redirectUrl);
    } catch (err) {
      return c.text(`failed to start login: ${String(err)}`, 502);
    }
    return c.redirect(authUrl);
  });

  // Session callback. sap redirects the browser here after storing the user's
  // OAuth session; the login token resolves to the authenticated DID, which we
  // bind to a fresh server session and hand the browser as a cookie before
  // sending it on to the app.
  app.get("/session/callback", async (c) => {
    const loginToken = c.req.query("login");
    if (!loginToken) {
      return c.text("missing login token", 400);
    }
    let did: string;
    try {
      did = await pear.resolveLogin(loginToken);
    } catch (err) {
      return c.text(`failed to resolve login: ${String(err)}`, 502);
    }
    const token = sessions.create(did);
    // SameSite=None + Secure so the docsv2 frontend (a different origin) can
    // send the cookie on credentialed XHR.
    setCookie(c, SESSION_COOKIE, token, {
      httpOnly: true,
      secure: true,
      sameSite: "None",
      path: "/",
    });
    return c.redirect(config.appOrigin);
  });

  // whoami reports the logged-in user for the current session, so the frontend
  // can gate its authenticated routes.
  app.get("/session/whoami", (c) => {
    const did = sessionDid(c);
    if (!did) {
      return c.json({ error: "AuthRequired" }, 401);
    }
    return c.json({ did });
  });

  app.post("/session/logout", (c) => {
    const token = getCookie(c, SESSION_COOKIE);
    if (token) {
      sessions.remove(token);
    }
    deleteCookie(c, SESSION_COOKIE, { path: "/" });
    return c.body(null, 204);
  });

  app.post("/xrpc/network.habitat.docs.createDoc", async (c) => {
    const caller = await resolveCaller(c, "network.habitat.docs.createDoc");
    const output: NetworkHabitatDocsCreateDoc.OutputSchema =
      await docs.createDoc(caller, orgFor(caller));
    return c.json(output);
  });

  // applyUpdate is the one endpoint that requires a server session: it writes
  // the CRDT record as the editing user, which needs their OAuth credential
  // (held by sap), not a service-auth JWT.
  app.post("/xrpc/network.habitat.docs.updateDoc", async (c) => {
    const caller = requireSession(c);
    const input =
      (await c.req.json()) as NetworkHabitatDocsUpdateDoc.InputSchema;
    const org = orgFor(caller);
    const allowed = await pear.check(
      org,
      caller,
      "writer",
      pear.spaceUri(input.docId, org),
    );
    if (!allowed) {
      throw new ForbiddenError("not a writer on this doc");
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
    const caller = await resolveCaller(c, "network.habitat.docs.listDocs");
    const spaces = await pear.listReadableSpaces(orgFor(caller), caller);
    const output: NetworkHabitatDocsListDocs.OutputSchema = {
      docs: meta.docsBySpaceUris(spaces),
    };
    return c.json(output);
  });

  // getRecord for a doc's CRDT is served from the server's merged state rather
  // than pear: the space no longer holds a single "self" CRDT record but one
  // per editor, and their merge is the canonical content the frontend reads.
  // Any other getRecord (e.g. a group profile for the share dialog) falls
  // through to the generic pear proxy below.
  app.get("/xrpc/network.habitat.space.getRecord", async (c, next) => {
    const collection = c.req.query("collection");
    const rkey = c.req.query("rkey");
    const space = c.req.query("space");
    if (collection !== CRDT_COLLECTION || rkey !== SELF || !space) {
      return next();
    }
    const caller = await resolveCaller(c, "network.habitat.space.getRecord");
    // Confirm the caller may read the doc before handing back merged content.
    const org = space.split("/")[2];
    const allowed = await pear.check(org, caller, "reader", space);
    if (!allowed) {
      throw new ForbiddenError("not a reader on this doc");
    }
    const blob = docs.stateB64(space);
    const output: NetworkHabitatSpaceGetRecord.OutputSchema = {
      uri: space,
      cid: "",
      value: blob ? { blob } : {},
    };
    return c.json(output);
  });

  // Everything else under /xrpc is proxied to pear through sap, authenticated
  // as the logged-in user. This lets the docsv2 frontend talk to the docs
  // server directly (relationship writes, space listings, group profiles, …)
  // and rely on its server session instead of a separate pear OAuth session.
  app.all("/xrpc/:nsid", async (c) => {
    const did = requireSession(c);
    const nsid = c.req.param("nsid");
    const url = new URL(c.req.url);
    const method = c.req.method;
    const body =
      method === "GET" || method === "HEAD"
        ? undefined
        : await c.req.arrayBuffer();
    // Forward only the headers pear needs: the content type and the
    // Atproto-Proxy target (which routes to other habitat services like the
    // home/groups server). The session cookie and CORS headers are dropped.
    const forward = new Headers();
    const ct = c.req.header("content-type");
    if (ct) {
      forward.set("content-type", ct);
    }
    const proxyTarget = c.req.header("atproto-proxy");
    if (proxyTarget) {
      forward.set("atproto-proxy", proxyTarget);
    }
    const resp = await pear.proxy(did, nsid, method, url.search, body, forward);
    const buf = await resp.arrayBuffer();
    const respCt = resp.headers.get("content-type");
    if (respCt) {
      c.header("content-type", respCt);
    }
    return c.body(buf, resp.status as ContentfulStatusCode);
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
