// Habitat OAuth scope grammar (see internal/oauthserver/permission.go):
//   "atproto"                              — base AT Protocol access
//   "org:<nsid-or-*>"                      — read access to an org's records
//   "org:<nsid-or-*>?action=create&action=update" — write access, per action
// This module parses that grammar to render a human-readable description per
// scope, similar to how the AT Protocol PDS explains requested scopes on its
// OAuth consent screen. It leans on @atproto/oauth-types for the general
// OAuth scope/client-id/client-metadata grammar (which Habitat's scope
// syntax is a specialization of) rather than reimplementing that parsing.
import {
  ATPROTO_SCOPE_VALUE,
  isConventionalOAuthClientId,
  isOAuthClientIdDiscoverable,
  isOAuthScope,
  oauthClientMetadataSchema,
  parseAtprotoLoopbackClientId,
  parseOAuthDiscoverableClientId,
  type OAuthClientMetadata,
} from "@atproto/oauth-types";

export interface ScopeDescription {
  scope: string;
  summary: string;
  detail?: string;
}

// describeScope renders one space-delimited scope token as a short summary
// plus (when there's more to say) a longer detail line, mirroring the level
// of detail atproto's own OAuth consent screen gives each requested scope.
export function describeScope(scope: string): ScopeDescription {
  if (scope === ATPROTO_SCOPE_VALUE) {
    return {
      scope,
      summary: "Basic account access",
      detail: "Read your DID and manage this app's session with Habitat.",
    };
  }

  const [positional, rawQuery] = splitOnce(scope, "?");
  const [resource, namespace] = splitOnce(positional, ":");
  if (resource === "org" && namespace) {
    const collection = namespace === "*" ? "any collection" : namespace;
    const actions = rawQuery
      ? [...new URLSearchParams(rawQuery).getAll("action")]
      : [];
    if (actions.length === 0) {
      return {
        scope,
        summary: `Read records in ${collection}`,
        detail: `View your organization's ${collection} records.`,
      };
    }
    const verbs = actions
      .map((action) => (action === "create" ? "create" : "update"))
      .join(" and ");
    return {
      scope,
      summary: `${capitalize(verbs)} records in ${collection}`,
      detail: `Allows this app to ${verbs} your organization's ${collection} records.`,
    };
  }

  // Unrecognized shape — still surface the raw scope rather than hiding it.
  return { scope, summary: scope };
}

// describeScopes parses a space-separated scope string (the shape validated
// by @atproto/oauth-types' isOAuthScope) into one description per token.
export function describeScopes(
  scopeString: string | undefined,
): ScopeDescription[] {
  if (!scopeString) return [];
  return scopeString
    .split(" ")
    .filter((s) => s.length > 0 && isOAuthScope(s))
    .map(describeScope);
}

function splitOnce(s: string, sep: string): [string, string | undefined] {
  const i = s.indexOf(sep);
  return i === -1 ? [s, undefined] : [s.slice(0, i), s.slice(i + 1)];
}

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

export interface ClientMetadataResult {
  clientId: string;
  metadata: OAuthClientMetadata;
  /** True when metadata was decoded from a loopback client_id rather than fetched. */
  loopback: boolean;
}

// fetchClientMetadata resolves a client_id to its client metadata document.
// Per the atproto/OAuth client-id-metadata-document spec, a "discoverable"
// client_id is itself the HTTPS URL of a publicly (CORS-enabled) fetchable
// JSON document; a loopback client_id (local dev clients) instead encodes
// its metadata directly in the URL and needs no network request.
export async function fetchClientMetadata(
  clientId: string,
): Promise<ClientMetadataResult> {
  if (
    isOAuthClientIdDiscoverable(clientId) ||
    isConventionalOAuthClientId(clientId)
  ) {
    const url = parseOAuthDiscoverableClientId(clientId);
    const res = await fetch(url.toString(), {
      headers: { Accept: "application/json" },
    });
    if (!res.ok) {
      throw new Error(`client metadata request failed: ${res.status}`);
    }
    const json = await res.json();
    const metadata = oauthClientMetadataSchema.parse(json);
    return { clientId, metadata, loopback: false };
  }

  // Not a discoverable https URL — try parsing it as a loopback client_id
  // (e.g. "http://localhost?redirect_uri=...&scope=..."), which embeds its
  // own metadata rather than pointing at a hosted document.
  const loopback = parseAtprotoLoopbackClientId(clientId);
  const metadata = oauthClientMetadataSchema.parse({
    client_id: clientId,
    client_name: "Local development client",
    redirect_uris: loopback.redirect_uris,
    scope: loopback.scope,
    grant_types: ["authorization_code", "refresh_token"],
    response_types: ["code"],
    token_endpoint_auth_method: "none",
    application_type: "native",
    dpop_bound_access_tokens: true,
  });
  return { clientId, metadata, loopback: true };
}
