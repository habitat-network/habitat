import clientMetadata from "./clientMetadata";
import * as client from "openid-client";
import { decodeJwt } from "jose";
import { create, StoreApi } from "zustand";
import { persist } from "zustand/middleware";

const stateLocalStorageKey = "state";
const serverDomainLocalStorageKey = "login-server-domain";

interface AuthInfo {
  did: string;
  accessToken: string;
  refreshToken: string | undefined;
  expiresAt: number; // epoch seconds
  serverDomain: string;
}

export class AuthManager {
  private appName: string;
  private domain: string;
  private defaultDomain: string;
  private store: StoreApi<{ authInfo: AuthInfo | undefined }> & {
    persist: { rehydrate: () => void | Promise<void> };
  };
  private onUnauthenticated: () => void;
  private refreshPromise: Promise<void> | undefined;

  constructor(
    appName: string,
    domain: string,
    defaultDomain: string,
    onUnauthenticated: () => void,
  ) {
    this.appName = appName;
    this.domain = domain;
    this.defaultDomain = defaultDomain;
    this.store = create(
      persist<{ authInfo: AuthInfo | undefined }>(
        () => ({ authInfo: undefined }),
        {
          name: `auth-info-${domain}`,
        },
      ),
    );

    this.onUnauthenticated = onUnauthenticated;
  }

  getAuthInfo() {
    return this.store.getState().authInfo;
  }

  // Builds a fresh openid-client Configuration pointed at the given OAuth
  // server domain. Called per-login/per-request instead of once at
  // construction, since the server domain now varies by which org/instance
  // the user is logging into.
  private buildConfig(serverDomain: string) {
    const { client_id } = clientMetadata(this.appName, this.domain);
    return new client.Configuration(
      {
        issuer: `https://${serverDomain}/oauth/authorize`,
        authorization_endpoint: `https://${serverDomain}/oauth/authorize`,
        token_endpoint: `https://${serverDomain}/oauth/token`,
      },
      client_id,
    );
  }

  loginUrl(handle: string, redirectUri: string, serverDomain?: string) {
    const resolvedDomain = serverDomain ?? this.defaultDomain;
    const state = client.randomState();
    localStorage.setItem(stateLocalStorageKey, state);
    localStorage.setItem(serverDomainLocalStorageKey, resolvedDomain);
    return client.buildAuthorizationUrl(this.buildConfig(resolvedDomain), {
      redirect_uri: redirectUri,
      response_type: "code",
      handle,
      state,
    });
  }

  logout = () => {
    // Delete all internal state
    this.store.setState({ authInfo: undefined });
    // Redirect to login page
    this.onUnauthenticated();
  };

  async maybeExchangeCode() {
    const url = new URL(window.location.href);
    const oauthError = url.searchParams.get("error");
    if (oauthError) {
      const description =
        url.searchParams.get("error_description") ?? oauthError;
      if (!window.location.pathname.includes("oauth-login")) {
        window.location.href = `/oauth-login?error=${encodeURIComponent(description)}`;
      }
      return false;
    }
    if (!url.searchParams.get("code") || !url.searchParams.get("state")) {
      return false;
    }
    const state = localStorage.getItem(stateLocalStorageKey);
    const serverDomain = localStorage.getItem(serverDomainLocalStorageKey);
    if (!state || !serverDomain) {
      // State (or the server domain stashed alongside it) is missing — the
      // browser session was cleared or a prior exchange failed. Redirect to
      // login so the user can retry.
      window.location.href = `/oauth-login?error=${encodeURIComponent("Login session expired. Please try again.")}`;
      return false;
    }
    const token = await client.authorizationCodeGrant(
      this.buildConfig(serverDomain),
      url,
      { expectedState: state },
    );
    // Only remove state after a successful exchange so a failed exchange
    // (network error, etc.) can be retried without losing the state.
    localStorage.removeItem(stateLocalStorageKey);
    localStorage.removeItem(serverDomainLocalStorageKey);
    this.setAuthState(token, serverDomain);
    // Remove code and state from URL
    url.searchParams.delete("code");
    url.searchParams.delete("state");
    url.searchParams.delete("scope");
    window.history.replaceState(null, "", url.toString());
    return true;
  }

  async fetch(
    url: string,
    method: string = "GET",
    body?: client.FetchBody,
    headers?: Headers,
    options?: client.DPoPOptions,
  ) {
    let { authInfo } = this.store.getState();
    if (!authInfo) {
      return this.handleUnauthenticated();
    }
    if (
      authInfo.refreshToken &&
      authInfo.expiresAt < Date.now() / 1000 + 5 * 60
    ) {
      if (!this.refreshPromise) {
        // Use the Web Locks API to serialize refresh across tabs: only one tab
        // acquires the lock and performs the refresh; others wait, then re-read
        // the fresh token written to localStorage by the winner.
        this.refreshPromise = navigator.locks
          .request("habitat-token-refresh", async () => {
            // Re-read after acquiring the lock — another tab may have already
            // refreshed while we were waiting.
            await this.store.persist.rehydrate();
            const currentInfo = this.store.getState().authInfo;
            if (
              !currentInfo?.refreshToken ||
              currentInfo.expiresAt >= Date.now() / 1000 + 5 * 60
            ) {
              return; // Token is already fresh; nothing to do.
            }
            const token = await client.refreshTokenGrant(
              this.buildConfig(currentInfo.serverDomain),
              currentInfo.refreshToken,
            );
            // Only write back if the user hasn't logged out in the meantime.
            if (this.store.getState().authInfo) {
              this.setAuthState(token, currentInfo.serverDomain);
            }
          })
          .then(() => {})
          .finally(() => {
            this.refreshPromise = undefined;
          });
      }
      try {
        await this.refreshPromise;
      } catch {
        return this.handleUnauthenticated();
      }
      // get the refreshed authInfo
      await this.store.persist.rehydrate();
      authInfo = this.store.getState().authInfo;
      if (!authInfo) {
        return this.handleUnauthenticated();
      }
    }
    if (!headers) {
      headers = new Headers();
    }
    headers.append("Habitat-Auth-Method", "oauth");
    const response = await client.fetchProtectedResource(
      this.buildConfig(authInfo.serverDomain),
      authInfo.accessToken,
      new URL(url, `https://${authInfo.serverDomain}`),
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

  private setAuthState(
    token: client.TokenEndpointResponse,
    serverDomain: string,
  ) {
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
      serverDomain,
    };
    this.store.setState({ authInfo: state });
    return state;
  }

  private handleUnauthenticated(): Response {
    this.logout();
    throw new UnauthenticatedError();
  }
}

export class UnauthenticatedError extends Error {}
