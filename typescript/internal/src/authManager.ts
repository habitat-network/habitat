import clientMetadata from "./clientMetadata";
import * as client from "openid-client";
import { decodeJwt } from "jose";
import { create, StoreApi } from "zustand";
import { persist } from "zustand/middleware";

const stateLocalStorageKey = "state";

interface AuthInfo {
  did: string;
  accessToken: string;
  refreshToken: string | undefined;
  expiresAt: number; // epoch seconds
}

export class AuthManager {
  private serverDomain: string;
  private store: StoreApi<{ authInfo: AuthInfo | undefined }>;
  private config: client.Configuration;
  private onUnauthenticated: () => void;
  private refreshPromise: Promise<void> | undefined;

  constructor(
    appName: string,
    domain: string,
    serverDomain: string,
    onUnauthenticated: () => void,
  ) {
    const { client_id } = clientMetadata(appName, domain);
    this.config = new client.Configuration(
      {
        issuer: `https://${serverDomain}/oauth/authorize`,
        authorization_endpoint: `https://${serverDomain}/oauth/authorize`,
        token_endpoint: `https://${serverDomain}/oauth/token`,
      },
      client_id,
    );
    this.serverDomain = serverDomain;
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
    if (!state) {
      // State is missing — the browser session was cleared or a prior exchange
      // failed. Redirect to login so the user can retry.
      window.location.href = `/oauth-login?error=${encodeURIComponent("Login session expired. Please try again.")}`;
      return false;
    }
    const token = await client.authorizationCodeGrant(this.config, url, {
      expectedState: state,
    });
    // Only remove state after a successful exchange so a failed exchange
    // (network error, etc.) can be retried without losing the state.
    localStorage.removeItem(stateLocalStorageKey);
    this.setAuthState(token);
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
            const currentInfo = this.store.getState().authInfo;
            if (
              !currentInfo?.refreshToken ||
              currentInfo.expiresAt >= Date.now() / 1000 + 5 * 60
            ) {
              return; // Token is already fresh; nothing to do.
            }
            const token = await client.refreshTokenGrant(
              this.config,
              currentInfo.refreshToken,
            );
            // Only write back if the user hasn't logged out in the meantime.
            if (this.store.getState().authInfo) {
              this.setAuthState(token);
            }
          })
          .then(() => { })
          .finally(() => {
            this.refreshPromise = undefined;
          });
      }
      try {
        await this.refreshPromise;
      } catch {
        // Safety net: if the refresh still failed (e.g. lock unavailable),
        // check whether another tab wrote a valid token before giving up.
        const freshInfo = this.store.getState().authInfo;
        if (!freshInfo || freshInfo.expiresAt <= Date.now() / 1000) {
          return this.handleUnauthenticated();
        }
      }
      // get the refreshed authInfo
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
      this.config,
      authInfo.accessToken,
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
    this.store.setState({ authInfo: state });
    return state;
  }

  private handleUnauthenticated(): Response {
    this.logout();
    throw new UnauthenticatedError();
  }
}

export class UnauthenticatedError extends Error { }
