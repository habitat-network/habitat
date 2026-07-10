import type { Fetcher } from "internal";

// DocsServerFetcher is the transport docsv2 hands to habitatClient. Instead of
// authenticating pear calls itself (the old AuthManager, OAuth against pear), it
// points every request straight at the docs server and relies on the server
// session established by the login flow. The docs server authenticates the
// request from its session cookie and, where needed, forwards it to pear using
// the user's credential it holds via sap.
//
// `credentials: "include"` sends the server-session cookie on these
// cross-origin requests; the docs server allows this origin with credentialed
// CORS.
export class DocsServerFetcher implements Fetcher {
  private baseUrl: string;
  private onUnauthenticated: () => void;

  constructor(baseUrl: string, onUnauthenticated: () => void) {
    this.baseUrl = baseUrl.replace(/\/$/, "");
    this.onUnauthenticated = onUnauthenticated;
  }

  async fetch(
    url: string,
    method: string = "GET",
    body?: BodyInit | null,
    headers?: Headers,
  ): Promise<Response> {
    const response = await fetch(`${this.baseUrl}${url}`, {
      method,
      body: body ?? undefined,
      headers,
      credentials: "include",
    });
    if (response.status === 401) {
      this.onUnauthenticated();
    }
    return response;
  }

  // whoami returns the logged-in user's DID, or undefined if there is no valid
  // server session. Used to gate the authenticated routes.
  async whoami(): Promise<string | undefined> {
    const response = await fetch(`${this.baseUrl}/session/whoami`, {
      credentials: "include",
    });
    if (!response.ok) {
      return undefined;
    }
    const { did } = (await response.json()) as { did: string };
    return did;
  }

  // loginUrl is the docs server endpoint that starts the OAuth flow for a
  // handle. Navigating the browser here (top-level) begins server-session login.
  loginUrl(handle: string): string {
    return `${this.baseUrl}/login?handle=${encodeURIComponent(handle)}`;
  }

  async logout(): Promise<void> {
    await fetch(`${this.baseUrl}/session/logout`, {
      method: "POST",
      credentials: "include",
    });
    this.onUnauthenticated();
  }
}
