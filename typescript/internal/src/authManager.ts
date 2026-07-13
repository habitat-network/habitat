import clientMetadata from "./clientMetadata";
import {
  BrowserOAuthClient,
  type OAuthSession,
} from "@atproto/oauth-client-browser";

export class AuthManager {
  private serverDomain: string;
  private client: BrowserOAuthClient;
  private session: OAuthSession | undefined;
  private onUnauthenticated: () => void;
  // Memoized so init() is safe to call from every router beforeLoad.
  private initPromise: Promise<void> | undefined;

  constructor(
    appName: string,
    domain: string,
    serverDomain: string,
    onUnauthenticated: () => void,
  ) {
    this.serverDomain = serverDomain;
    this.onUnauthenticated = onUnauthenticated;
    this.client = new BrowserOAuthClient({
      clientMetadata: clientMetadata(appName, domain),
      // Habitat resolves handles server-side; the client only needs to reach
      // the pear domain's OAuth metadata.
      handleResolver: `https://${serverDomain}`,
    });
  }

  // Processes an OAuth callback if present in the URL, otherwise restores an
  // existing session. Idempotent: the underlying client.init() must run once.
  init(): Promise<void> {
    if (!this.initPromise) {
      this.initPromise = this.client.init().then((result) => {
        this.session = result?.session;
      });
    }
    return this.initPromise;
  }

  getAuthInfo() {
    if (!this.session) {
      return undefined;
    }
    return { did: this.session.sub };
  }

  // Begins the OAuth flow against the pear domain as a service URL. The atproto
  // client resolves the pear domain's oauth-protected-resource -> authorization
  // server and runs PAR/PKCE/DPoP against Habitat. No login_hint is sent; the
  // user picks their handle on Habitat's disambiguation page.
  login() {
    void this.client.signInRedirect(`https://${this.serverDomain}`);
  }

  logout = () => {
    void this.session?.signOut();
    this.session = undefined;
    this.onUnauthenticated();
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
    const response = await this.session.fetchHandler(
      new URL(path, `https://${this.serverDomain}`).toString(),
      { method, body, headers },
    );
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
