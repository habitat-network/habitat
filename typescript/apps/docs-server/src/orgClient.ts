import fs from "node:fs/promises";
import * as client from "openid-client";
import { decodeJwt, exportJWK, importJWK, generateKeyPair, type JWK } from "jose";
import type { DerivedConfig } from "./config";

const KID = "docs-server";

interface Credential {
  accessToken: string;
  refreshToken: string;
  // epoch seconds
  expiresAt: number;
}

// OrgClient holds the org's OAuth credential and makes authenticated XRPC calls
// to pear on the org's behalf. It mirrors internal/sap/org_manager.go: a
// confidential OAuth client (private_key_jwt) that completes an authorization
// code flow once, then auto-refreshes the access token. Every request carries
// `Authorization: Bearer <token>` and `Habitat-Auth-Method: oauth`.
export class OrgClient {
  private config: DerivedConfig;
  private oauthConfig: client.Configuration;
  private publicJwk: JWK;
  private credential: Credential | undefined;
  private refreshing: Promise<void> | undefined;

  private constructor(
    config: DerivedConfig,
    oauthConfig: client.Configuration,
    publicJwk: JWK,
    credential: Credential | undefined,
  ) {
    this.config = config;
    this.oauthConfig = oauthConfig;
    this.publicJwk = publicJwk;
    this.credential = credential;
  }

  static async create(config: DerivedConfig): Promise<OrgClient> {
    const { privateKey, publicJwk } = await loadOrCreateKey(config.keyPath);
    const oauthConfig = new client.Configuration(
      {
        issuer: `${config.pearHost}/oauth/authorize`,
        authorization_endpoint: `${config.pearHost}/oauth/authorize`,
        token_endpoint: `${config.pearHost}/oauth/token`,
      },
      config.clientId,
      undefined,
      client.PrivateKeyJwt(privateKey, {
        [client.modifyAssertion]: (header) => {
          header.kid = KID;
        },
      }),
    );
    const credential = await loadCredential(config.credentialPath);
    return new OrgClient(config, oauthConfig, publicJwk, credential);
  }

  isAuthorized(): boolean {
    return this.credential !== undefined;
  }

  // clientMetadata is served at /client-metadata.json so pear can fetch it to
  // learn this confidential client's redirect URI and public signing key.
  clientMetadata() {
    return {
      client_id: this.config.clientId,
      client_name: "Habitat Docs Server",
      redirect_uris: [this.config.redirectUri],
      grant_types: ["authorization_code", "refresh_token"],
      response_types: ["code"],
      token_endpoint_auth_method: "private_key_jwt",
      token_endpoint_auth_signing_alg: "ES256",
      application_type: "web",
      dpop_bound_access_tokens: false,
      jwks: { keys: [{ ...this.publicJwk, kid: KID, use: "sig", alg: "ES256" }] },
    };
  }

  // beginAuth builds the authorization URL the org admin is redirected to. The
  // PKCE verifier and state are returned so the caller can stash them until the
  // callback.
  async beginAuth(): Promise<{ url: string; state: string; verifier: string }> {
    const verifier = client.randomPKCECodeVerifier();
    const challenge = await client.calculatePKCECodeChallenge(verifier);
    const state = client.randomState();
    const url = client.buildAuthorizationUrl(this.oauthConfig, {
      redirect_uri: this.config.redirectUri,
      response_type: "code",
      handle: this.config.orgHandle,
      code_challenge: challenge,
      code_challenge_method: "S256",
      state,
    });
    return { url: url.href, state, verifier };
  }

  // completeAuth exchanges the authorization code for tokens and persists them.
  async completeAuth(
    currentUrl: URL,
    expectedState: string,
    verifier: string,
  ): Promise<void> {
    const tokens = await client.authorizationCodeGrant(
      this.oauthConfig,
      currentUrl,
      { expectedState, pkceCodeVerifier: verifier },
    );
    await this.persistTokens(tokens);
  }

  // orgFetch performs an authenticated request to pear, refreshing the token if
  // needed and attaching the org credential headers.
  async orgFetch(url: string, init?: RequestInit): Promise<Response> {
    const token = await this.accessToken();
    const headers = new Headers(init?.headers);
    headers.set("Authorization", `Bearer ${token}`);
    headers.set("Habitat-Auth-Method", "oauth");
    return fetch(url, { ...init, headers });
  }

  // orgDid returns the org's DID, taken from the access token subject. The org
  // owns the canonical doc records, so this is the repo passed to getRecord.
  async orgDid(): Promise<string> {
    const token = await this.accessToken();
    const sub = decodeJwt(token).sub;
    if (!sub) {
      throw new Error("access token missing sub");
    }
    return sub;
  }

  private async accessToken(): Promise<string> {
    if (!this.credential) {
      throw new Error("docs server is not authorized; complete /oauth/login");
    }
    if (this.credential.expiresAt < Date.now() / 1000 + 60) {
      if (!this.refreshing) {
        this.refreshing = this.refresh().finally(() => {
          this.refreshing = undefined;
        });
      }
      await this.refreshing;
    }
    return this.credential.accessToken;
  }

  private async refresh(): Promise<void> {
    if (!this.credential) {
      throw new Error("no credential to refresh");
    }
    const tokens = await client.refreshTokenGrant(
      this.oauthConfig,
      this.credential.refreshToken,
    );
    await this.persistTokens(tokens);
  }

  private async persistTokens(
    tokens: client.TokenEndpointResponse,
  ): Promise<void> {
    if (!tokens.refresh_token) {
      throw new Error("token response missing refresh_token");
    }
    const decoded = decodeJwt(tokens.access_token);
    if (!decoded.exp) {
      throw new Error("access token missing exp");
    }
    this.credential = {
      accessToken: tokens.access_token,
      refreshToken: tokens.refresh_token,
      expiresAt: decoded.exp,
    };
    await fs.mkdir(this.config.dataDir, { recursive: true });
    await fs.writeFile(
      this.config.credentialPath,
      JSON.stringify(this.credential),
      "utf8",
    );
  }
}

async function loadCredential(
  credentialPath: string,
): Promise<Credential | undefined> {
  try {
    const raw = await fs.readFile(credentialPath, "utf8");
    return JSON.parse(raw) as Credential;
  } catch {
    return undefined;
  }
}

async function loadOrCreateKey(
  keyPath: string,
): Promise<{ privateKey: CryptoKey; publicJwk: JWK }> {
  let privateJwk: JWK;
  try {
    privateJwk = JSON.parse(await fs.readFile(keyPath, "utf8")) as JWK;
  } catch {
    const { privateKey } = await generateKeyPair("ES256", {
      extractable: true,
    });
    privateJwk = await exportJWK(privateKey);
    await fs.mkdir(keyPath.replace(/\/[^/]*$/, ""), { recursive: true });
    await fs.writeFile(keyPath, JSON.stringify(privateJwk), "utf8");
  }
  const privateKey = (await importJWK(privateJwk, "ES256")) as CryptoKey;
  // Strip private fields to derive the public JWK published in client metadata.
  const { d: _d, ...publicJwk } = privateJwk;
  return { privateKey, publicJwk };
}
