import { verifySignature } from "@atproto/crypto";
import type { DerivedConfig } from "./config";
import type { OrgClient } from "./orgClient";

// ServiceAuthError signals an invalid/forbidden service-auth JWT (HTTP 401).
export class ServiceAuthError extends Error {}

interface ServiceAuthPayload {
  iss: string;
  aud: string;
  exp: number;
  lxm?: string;
}

// ServiceAuthVerifier fully verifies the service-auth JWT that pear signs on a
// caller's behalf and forwards to us: it checks the audience (our DID), the
// lexicon method, expiry, and the cryptographic signature against the caller's
// signing key. The caller is an org member whose did:web doc is served by pear
// behind the org OAuth credential, so we resolve it through the OrgClient.
export class ServiceAuthVerifier {
  private config: DerivedConfig;
  private org: OrgClient;

  constructor(config: DerivedConfig, org: OrgClient) {
    this.config = config;
    this.org = org;
  }

  // verify returns the caller (issuer) DID on success.
  async verify(jwt: string, expectedLxm: string): Promise<string> {
    const parts = jwt.split(".");
    if (parts.length !== 3) {
      throw new ServiceAuthError("malformed JWT");
    }
    const payload = decodeJson<ServiceAuthPayload>(parts[1]);

    if (payload.aud !== this.config.did) {
      throw new ServiceAuthError(
        `wrong audience: ${payload.aud} != ${this.config.did}`,
      );
    }
    if (payload.lxm !== undefined && payload.lxm !== expectedLxm) {
      throw new ServiceAuthError(`wrong lxm: ${payload.lxm}`);
    }
    if (typeof payload.exp !== "number" || payload.exp < Date.now() / 1000) {
      throw new ServiceAuthError("token expired");
    }

    const signingKey = await this.resolveSigningKey(payload.iss);
    const data = new TextEncoder().encode(`${parts[0]}.${parts[1]}`);
    const sig = base64UrlToBytes(parts[2]);
    const ok = await verifySignature(signingKey, data, sig);
    if (!ok) {
      throw new ServiceAuthError("invalid signature");
    }
    return payload.iss;
  }

  // resolveSigningKey fetches the caller's did:web document through pear (which
  // gates member DID docs behind the org credential) and extracts the atproto
  // signing key as a did:key string.
  private async resolveSigningKey(did: string): Promise<string> {
    if (!did.startsWith("did:web:")) {
      throw new ServiceAuthError(`unsupported issuer DID method: ${did}`);
    }
    const url = didWebDocUrl(did);
    const res = await this.org.orgFetch(url);
    if (!res.ok) {
      throw new ServiceAuthError(
        `failed to resolve issuer DID doc (${res.status})`,
      );
    }
    const doc = (await res.json()) as {
      verificationMethod?: {
        id: string;
        publicKeyMultibase?: string;
      }[];
    };
    const methods = doc.verificationMethod ?? [];
    const vm =
      methods.find((m) => m.id.endsWith("#atproto")) ??
      methods.find((m) => m.publicKeyMultibase);
    if (!vm?.publicKeyMultibase) {
      throw new ServiceAuthError("no signing key in issuer DID doc");
    }
    return `did:key:${vm.publicKeyMultibase}`;
  }
}

function didWebDocUrl(did: string): string {
  const id = did.slice("did:web:".length);
  const segments = id.split(":").map(decodeURIComponent);
  const host = segments[0];
  if (segments.length === 1) {
    return `https://${host}/.well-known/did.json`;
  }
  return `https://${host}/${segments.slice(1).join("/")}/did.json`;
}

function decodeJson<T>(b64url: string): T {
  return JSON.parse(
    Buffer.from(b64url, "base64url").toString("utf8"),
  ) as T;
}

function base64UrlToBytes(b64url: string): Uint8Array {
  return new Uint8Array(Buffer.from(b64url, "base64url"));
}
