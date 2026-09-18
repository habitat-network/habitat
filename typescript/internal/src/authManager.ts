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
  // existing session. Idempotent: the underlying client.init() must run once.
  async init(): Promise<void> {
    try {
      const result = await this.client.init();
      this.session = result?.session;
    } catch (err) {
      // The provider (or the user) rejected the auth request. Route back to
      // login the same way any other unauthenticated state does, carrying
      // the reason along.
      if (err instanceof OAuthCallbackError) {
        this.onUnauthenticated(err.message);
        return;
      }
      throw err;
    }
  }

  getAuthInfo() {
    if (!this.session) {
      return undefined;
    }
    return { did: this.session.did };
  }

  login(handle: string, redirectUrl?: string) {
    return this.client.signInRedirect(handle, {
      redirect_uri: redirectUrl
        ? oauthRedirectUriSchema.parse(redirectUrl)
        : undefined,
    });
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
