import { IdResolver } from "@atproto/identity";
import clientMetadata from "./clientMetadata";
import {
  BrowserOAuthClient,
  isAtprotoDid,
  type AtprotoDid,
  type OAuthSession,
} from "@atproto/oauth-client-browser";
import { normalizeHandle } from '@atproto/syntax'

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
    const internalResolver = new IdResolver()
    this.client = new BrowserOAuthClient({
      clientMetadata: clientMetadata(appName, domain),
      identityResolver: {
        resolve: async (identifier) => {
          let did: AtprotoDid
          if (!isAtprotoDid(identifier)) {
            const result = await internalResolver.handle.resolve(identifier);
            if (!result || !isAtprotoDid(result)) {
              throw new Error(`Handle not found: ${identifier}`);
            }
            did = result
          } else {
            did = identifier
          }
          const id = await internalResolver.did.resolve(did)
          const handle = id?.alsoKnownAs?.[0] ? id.alsoKnownAs[0].replace('at://', '') : "invalid.handle"
          console.log({ did, handle })
          return {
            did: did,
            handle: handle,
            didDoc: {
              id: did,
              service: [
                {
                  id: "#atproto_pds",
                  type: "AtprotoPersonalDataServer",
                  serviceEndpoint: `https://${serverDomain}`,
                },
              ]
            }
          }
        }
      },
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

  login(handle: string) {
    console.log(handle)
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

export class UnauthenticatedError extends Error { }
