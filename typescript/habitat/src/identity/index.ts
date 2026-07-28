export {
  DEFAULT_HABITAT_SERVICE_URL,
  HabitatIdentityResolver,
} from "./habitat-identity-resolver.js";
export { HabitatIdentityResolverError } from "./errors.js";
export type { HabitatIdentityResolverErrorOptions } from "./errors.js";

/**
 * Re-exported so consumers can type against the resolver without adding a
 * direct `@atproto-labs/identity-resolver` import.
 */
export type {
  AtprotoDid,
  AtprotoDidDocument,
  IdentityInfo,
  IdentityResolver,
  ResolveIdentityOptions,
} from "@atproto-labs/identity-resolver";
