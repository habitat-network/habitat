import clientMetadata from "./clientMetadata";
import * as client from "openid-client";
import { HabitatClient, HabitatAuthedAgentSession } from "./habitatClient";
import { DidResolver } from "@atproto/identity";
import { Agent } from "@atproto/api";

const handleLocalStorageKey = "handle";
const didLocalStorageKey = "did";
const tokenLocalStorageKey = "token";
const stateLocalStorageKey = "state";

export class AuthManager {
  handle: string | null;
  did: string | null;

  private serverDomain: string;
  private accessToken: string | null = null;
  private config: client.Configuration;
  private onUnauthenticated: () => void;

  constructor(
    domain: string,
    serverDomain: string,
    onUnauthenticated: () => void,
  ) {
    const { client_id } = clientMetadata(domain);
    this.config = new client.Configuration(
      {
        issuer: `https://${serverDomain}/oauth/authorize`,
        authorization_endpoint: `https://${serverDomain}/oauth/authorize`,
        token_endpoint: `https://${serverDomain}/oauth/token`,
      },
      client_id,
    );
    this.handle = localStorage.getItem(handleLocalStorageKey);
    this.did = localStorage.getItem(didLocalStorageKey);
    this.accessToken = localStorage.getItem(tokenLocalStorageKey);
    this.serverDomain = serverDomain;

    this.onUnauthenticated = onUnauthenticated;
  }

  isAuthenticated() {
    return !!this.accessToken;
  }

  loginUrl(handle: string, redirectUri: string) {
    this.handle = handle;
    localStorage.setItem(handleLocalStorageKey, handle);
    const state = client.randomState();
    localStorage.setItem(stateLocalStorageKey, state);
    return client.buildAuthorizationUrl(this.config, {
      redirect_uri: redirectUri,
      response_type: "code",
      handle,
      state,
    });
  }

  logout() {
    // TODO: implement me!!
  }

  async maybeExchangeCode(currentUrl: string) {
    const url = new URL(currentUrl);
    if (!url.searchParams.get("code") || !url.searchParams.get("state")) {
      return;
    }
    const state = localStorage.getItem(stateLocalStorageKey);
    if (!state) {
      throw new Error("No state found");
    }
    localStorage.removeItem(stateLocalStorageKey);
    const token = await client.authorizationCodeGrant(
      this.config,
      new URL(currentUrl),
      {
        expectedState: state,
      },
    );
    this.accessToken = token.access_token;

    localStorage.setItem(tokenLocalStorageKey, token.access_token);
    localStorage.setItem(didLocalStorageKey, token.sub as string);

    window.location.href = "/";
  }

  client(): HabitatClient {
    const serverUrl = "https://" + this.serverDomain;
    const authedSession = new HabitatAuthedAgentSession(serverUrl, this);
    const authedAgent = new Agent(authedSession);
    if (!this.did) {
      throw new Error("No DID found");
    }
    return new HabitatClient(this.did, authedAgent, new DidResolver({}));
  }

  async fetch(
    url: string,
    method: string = "GET",
    body?: client.FetchBody,
    headers?: Headers,
    options?: client.DPoPOptions,
  ) {
    if (!this.accessToken) {
      return this.handleUnauthenticated();
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
      return this.handleUnauthenticated();
    }
    return response;
  }

  private handleUnauthenticated(): Response {
    this.handle = null;
    this.accessToken = null;
    localStorage.removeItem(handleLocalStorageKey);
    localStorage.removeItem(tokenLocalStorageKey);
    this.onUnauthenticated();
    throw new UnauthenticatedError();
  }
}

export class UnauthenticatedError extends Error {}
