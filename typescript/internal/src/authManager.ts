import clientMetadata from "./clientMetadata";
import {
  BrowserOAuthClient,
  type AtprotoDid,
  type AtprotoDidDocument,
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
    const did: AtprotoDid = `did:web:${serverDomain}`;
    // Habitat is a brokering OAuth server: the frontend authenticates against
    // Habitat's own OAuth server (the pear domain), which then brokers to the
    // user's real PDS. The atproto client normally resolves a handle to the
    // user's real PDS/authorization server; we override identity resolution so
    // every handle resolves to a synthetic DID document whose PDS is the pear
    // domain. That makes the client run PAR/PKCE/DPoP against Habitat while
    // still forwarding the typed handle to Habitat as the OAuth login_hint.
    const identityResolver = {
      async resolve(handle: string) {
        const didDoc: AtprotoDidDocument = {
          id: did,
          service: [
            {
              id: "#atproto_pds",
              type: "AtprotoPersonalDataServer",
              serviceEndpoint: `https://${serverDomain}`,
            },
          ],
        };
        return { did, handle, didDoc };
      },
    };
    this.client = new BrowserOAuthClient({
      clientMetadata: clientMetadata(appName, domain),
      identityResolver,
      // Return the authorization code in the query string, not the URL fragment:
      // fosite rejects fragment mode for this client, and the frontend uses hash
      // routing, so callback params in the fragment would collide with the router.
      responseMode: "query",
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

  // Begins the OAuth flow for the given handle. Identity resolution is
  // overridden (see constructor) so this runs against the pear domain; the
  // handle is forwarded to Habitat as the login_hint. Returns the redirect
  // promise so callers can surface resolution/PAR errors.
  login(handle: string) {
    return this.client.signInRedirect(handle);
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
