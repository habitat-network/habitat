import { type Agent, type DidString, type FetchHandler } from "@atproto/lex";
import {
  BrowserOAuthClient,
  OAuthCallbackError,
  oauthRedirectUriSchema,
  type OAuthSession,
} from "@atproto/oauth-client-browser";
import { HabitatIdentityResolver } from "@habitat-network/habitat";
import clientMetadata from "./clientMetadata";

export class AuthManager implements Agent {
  private client: BrowserOAuthClient;
  private session: OAuthSession | undefined;
  private onUnauthenticated: (error?: string) => void;
  private initPromise: Promise<void> | undefined;
  private serverUrl: string;

  get did(): DidString | undefined {
    return this.session?.did as DidString | undefined;
  }

  // Implements @atproto/lex's Agent.fetchHandler so AuthManager can be passed
  // straight to `xrpc()`.
  fetchHandler: FetchHandler = (path, init): Promise<Response> =>
    this.fetch(path, init.method, init.body, new Headers(init.headers));

  constructor(
    appName: string,
    baseUrl: string,
    serverUrl: string,
    onUnauthenticated: (error?: string) => void,
  ) {
    this.onUnauthenticated = onUnauthenticated;
    this.serverUrl = serverUrl;
    this.client = new BrowserOAuthClient({
      clientMetadata: clientMetadata(appName, baseUrl),
      // Resolve handles/DIDs through the habitat instance's own identity
      // endpoint rather than the public network, since it also hosts
      // identities the public network doesn't know about.
      identityResolver: new HabitatIdentityResolver(serverUrl),
      // Return the authorization code in the query string, not the URL fragment:
      // fosite rejects fragment mode for this client, and the frontend uses hash
      // routing, so callback params in the fragment would collide with the router.
      responseMode: "query",
    });
  }

  // Processes an OAuth callback if present in the URL, otherwise restores an
  // existing session. The underlying client.init() must run exactly once (it
  // consumes the single-use authorization code in the URL), but TanStack
  // Router's root beforeLoad can invoke this multiple times concurrently
  // (route re-evaluation, React StrictMode). Memoize the call so every
  // caller awaits the same run instead of racing to set `this.session` from
  // their own independent client.init() call.
  init(): Promise<void> {
    if (!this.initPromise) {
      this.initPromise = this.doInit();
    }
    return this.initPromise;
  }

  private async doInit(): Promise<void> {
    try {
      const result = await this.client.init();
      this.session = result?.session;
    } catch (err) {
      // Either the provider (or the user) rejected the auth request, or a
      // previously persisted session could no longer be restored (e.g. an
      // expired/revoked refresh token). Both leave us unauthenticated, so
      // route back to login the same way any other unauthenticated state
      // does, carrying the reason along when there is one.
      this.onUnauthenticated(
        err instanceof OAuthCallbackError ? err.message : undefined,
      );
    }
  }

  getAuthInfo() {
    if (!this.session) {
      return undefined;
    }
    return { did: this.session.did };
  }

  async login(handle: string, redirectUrl?: string) {
    const options = {
      redirect_uri: redirectUrl
        ? oauthRedirectUriSchema.parse(redirectUrl)
        : undefined,
    };
    try {
      return await this.client.signInRedirect(handle, options);
    } catch (err) {
      if (!hasXrpcError(err, "EmailNotProvisioned")) {
        throw err;
      }
    }
    // A work email at a domain set up for sign-in that has never signed in
    // has no identity yet: the habitat instance only mints one once sign-in
    // verifies the email. With no DID to resolve, start the flow against the
    // instance itself. The OAuth client won't forward our own login_hint, so
    // hand the email over through the authorize endpoint's disambiguation
    // param instead (the same one its handle prompt submits).
    const url = await this.client.authorize(this.serverUrl, options);
    url.searchParams.set("disambiguation", handle);
    window.location.href = url.href;
  }

  logout = (error?: string) => {
    void this.session?.signOut();
    this.session = undefined;
    this.onUnauthenticated(error);
  };

  async fetch(
    path: string,
    method: string = "GET",
    body?: BodyInit | null,
    headers?: Headers,
  ) {
    if (!this.session) {
      return this.handleUnauthenticated();
    }
    if (!headers) {
      headers = new Headers();
    }
    headers.append("Habitat-Auth-Method", "oauth");
    const response = await this.session.fetchHandler(path, {
      method,
      body,
      headers,
    });
    if (response.status === 401) {
      return this.handleUnauthenticated();
    }
    return response;
  }

  private handleUnauthenticated(): Response {
    this.logout();
    throw new UnauthenticatedError();
  }
}

export class UnauthenticatedError extends Error {}

// hasXrpcError reports whether err, or anything in its cause chain, carries
// the XRPC error code (as HabitatIdentityResolverError does). BrowserOAuthClient
// wraps resolver failures, so the resolver's error is usually a cause.
export function hasXrpcError(err: unknown, code: string): boolean {
  for (let e = err; e instanceof Error; e = e.cause) {
    if ((e as { xrpcError?: unknown }).xrpcError === code) {
      return true;
    }
  }
  return false;
}
