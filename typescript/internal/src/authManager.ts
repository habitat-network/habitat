import clientMetadata from "./clientMetadata";
import * as client from "openid-client";
import { decodeJwt } from "jose";
import { HabitatClient, HabitatAuthedAgentSession } from "./habitatClient";
import { DidResolver } from "@atproto/identity";
import { Agent } from "@atproto/api";
import { create } from "zustand";
import { persist } from "zustand/middleware";

const stateLocalStorageKey = "state";

interface AuthInfo {
  did: string;
  accessToken: string;
  refreshToken: string | undefined;
  expiresAt: number;
}

export class AuthManager {
  private serverDomain: string;
  private store = create(
    persist<AuthInfo | null>(() => null, { name: "auth-info" }),
  );
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
    this.serverDomain = serverDomain;

    this.onUnauthenticated = onUnauthenticated;
  }

  getAuthInfo() {
    return this.store.getState();
  }

  loginUrl(handle: string, redirectUri: string) {
    const state = client.randomState();
    localStorage.setItem(stateLocalStorageKey, state);
    return client.buildAuthorizationUrl(this.config, {
      redirect_uri: redirectUri,
      response_type: "code",
      handle,
      state,
    });
  }

  logout = () => {
    // Delete all internal state
    this.store.setState(null);
    // Redirect to login page
    this.onUnauthenticated();
  };

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
    this.setAuthState(token);
    window.location.href = "/";
  }

  client(): HabitatClient {
    const serverUrl = "https://" + this.serverDomain;
    const authedSession = new HabitatAuthedAgentSession(serverUrl, this);
    const authedAgent = new Agent(authedSession);
    const did = this.store.getState()?.did;
    if (!did) {
      throw new Error("No DID found");
    }
    return new HabitatClient(did, authedAgent, new DidResolver({}));
  }

  async fetch(
    url: string,
    method: string = "GET",
    body?: client.FetchBody,
    headers?: Headers,
    options?: client.DPoPOptions,
  ) {
    let state = this.store.getState();
    if (!state) {
      return this.handleUnauthenticated();
    }
    if (state.refreshToken && state.expiresAt < Date.now() / 1000 + 5 * 60) {
      try {
        const token = await client.refreshTokenGrant(
          this.config,
          state.refreshToken,
        );
        state = this.setAuthState(token);
      } catch (e) {
        return this.handleUnauthenticated();
      }
    }
    if (!headers) {
      headers = new Headers();
    }
    headers.append("Habitat-Auth-Method", "oauth");
    const response = await client.fetchProtectedResource(
      this.config,
      state.accessToken,
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

  private setAuthState(token: client.TokenEndpointResponse) {
    // The DID is encoded in the sub claim of the JWT
    const decoded = decodeJwt(token.access_token);
    if (!decoded.sub || !decoded.exp) {
      throw new Error("Invalid token");
    }
    const state = {
      did: decoded.sub,
      accessToken: token.access_token,
      refreshToken: token.refresh_token,
      expiresAt: decoded.exp,
    };
    this.store.setState(state);
    return state;
  }

  private handleUnauthenticated(): Response {
    this.logout();
    throw new UnauthenticatedError();
  }
}

export class UnauthenticatedError extends Error { }
