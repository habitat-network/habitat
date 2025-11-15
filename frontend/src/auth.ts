import clientMetadata from "../client-metadata";
import * as client from "openid-client";

const handleLocalStorageKey = "handle";
const tokenLocalStorageKey = "token";
const stateLocalStorageKey = "state";

export class AuthManager {
  handle: string | null;
  private serverDomain: string;
  private accessToken: string | null;
  private config: client.Configuration;
  private onUnauthenticated: () => void;

  constructor(serverDomain: string, onUnauthenticated: () => void) {
    const { client_id } = clientMetadata(__DOMAIN__);
    this.config = new client.Configuration(
      {
        issuer: `https://${serverDomain}/oauth/authorize`,
        authorization_endpoint: `https://${serverDomain}/oauth/authorize`,
        token_endpoint: `https://${serverDomain}/oauth/token`,
      },
      client_id,
    );
    this.handle = localStorage.getItem(handleLocalStorageKey);
    this.accessToken = localStorage.getItem(tokenLocalStorageKey);
    this.onUnauthenticated = onUnauthenticated;
    this.serverDomain = serverDomain;
  }

  isAuthenticated() {
    return !!this.accessToken;
  }

  loginUrl(handle: string, redirectUri: string) {
    this.handle = handle;
    localStorage.setItem(handleLocalStorageKey, handle);
    const state = client.randomState();
    console.log(state);
    localStorage.setItem(stateLocalStorageKey, state);
    return client.buildAuthorizationUrl(this.config, {
      redirect_uri: redirectUri,
      response_type: "code",
      handle,
      state,
    });
  }
  async exchangeCode(currentUrl: string) {
    const state = localStorage.getItem(stateLocalStorageKey);
    if (!state) {
      throw new Error("No state found");
    }
    localStorage.removeItem(stateLocalStorageKey);
    console.log(currentUrl);
    console.log(state);
    const token = await client.authorizationCodeGrant(
      this.config,
      new URL(currentUrl),
      {
        expectedState: state,
      },
    );
    this.accessToken = token.access_token;
    localStorage.setItem(tokenLocalStorageKey, token.access_token);
  }

  async fetch(
    url: string,
    method: string = "GET",
    body?: client.FetchBody,
    headers?: Headers,
    options?: client.DPoPOptions,
  ) {
    if (!this.accessToken) {
      this.handleUnauthenticated();
      return;
    }
    if (!headers) {
      headers = new Headers();
    }
    headers.append("Habitat-Auth-Method", "oauth");
    const response = await client.fetchProtectedResource(
      this.config,
      this.accessToken,
      new URL(url, `https://${this.serverDomain}`),
      method,
      body,
      headers,
      options,
    );

    if (response.status === 401) {
      this.handleUnauthenticated();
      return;
    }
    return response;
  }

  private handleUnauthenticated() {
    this.handle = null;
    this.accessToken = null;
    localStorage.removeItem(handleLocalStorageKey);
    localStorage.removeItem(tokenLocalStorageKey);
    this.onUnauthenticated();
    throw new UnauthenticatedError();
  }
}

export class UnauthenticatedError extends Error { }
