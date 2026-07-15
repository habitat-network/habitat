import { IdResolver } from "@atproto/identity";
import clientMetadata from "./clientMetadata";
import {
  BrowserOAuthClient,
  isAtprotoDid,
  type AtprotoDid,
  type OAuthSession,
} from "@atproto/oauth-client-browser";

export class AuthManager {
  private serverUrl: string;
  private client: BrowserOAuthClient;
  private session: OAuthSession | undefined;
  private onUnauthenticated: () => void;

  constructor(
    appName: string,
    baseUrl: string,
    serverUrl: string,
    onUnauthenticated: () => void,
  ) {
    this.serverUrl = serverUrl;
    this.onUnauthenticated = onUnauthenticated;
    const internalResolver = new IdResolver();
    this.client = new BrowserOAuthClient({
      clientMetadata: clientMetadata(appName, baseUrl),
      identityResolver: {
        resolve: async (identifier) => {
          let did: AtprotoDid;
          if (!isAtprotoDid(identifier)) {
            const result = await internalResolver.handle.resolve(identifier);
            if (!result || !isAtprotoDid(result)) {
              throw new Error(`Handle not found: ${identifier}`);
            }
            did = result;
          } else {
            did = identifier;
          }
          const id = await internalResolver.did.resolve(did);
          const handle = id?.alsoKnownAs?.[0]
            ? id.alsoKnownAs[0].replace("at://", "")
            : "invalid.handle";
          console.log({ did, handle });
          return {
            did: did,
            handle: handle,
            didDoc: {
              id: did,
              service: [
                {
                  id: "#atproto_pds",
                  type: "AtprotoPersonalDataServer",
                  serviceEndpoint: serverUrl,
                },
              ],
            },
          };
        },
      },
      // Return the authorization code in the query string, not the URL fragment:
      // fosite rejects fragment mode for this client, and the frontend uses hash
      // routing, so callback params in the fragment would collide with the router.
      responseMode: "query",
    });
  }

  // Processes an OAuth callback if present in the URL, otherwise restores an
  // existing session. Idempotent: the underlying client.init() must run once.
  async init(): Promise<void> {
    const result = await this.client.init();
    this.session = result?.session;
  }

  getAuthInfo() {
    if (!this.session) {
      return undefined;
    }
    return { did: this.session.sub };
  }

  login(handle: string) {
    console.log(handle);
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
      new URL(path, this.serverUrl).toString(),
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
