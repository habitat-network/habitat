/**
 * Inlined rather than imported from `@atproto-labs/identity-resolver`, which is
 * a type-only peer dependency. The value is fixed by the AT Protocol spec.
 */
export const HANDLE_INVALID = "handle.invalid";

const MAX_HANDLE_LENGTH = 253;

/**
 * The AT Protocol handle grammar: two or more dot-separated segments of
 * alphanumerics and hyphens, each 1-63 characters and neither starting nor
 * ending with a hyphen, with a final segment that does not start with a digit.
 *
 * Note that `handle.invalid` itself satisfies this grammar, so the passthrough
 * case needs no special-casing.
 */
const HANDLE_REGEX =
  /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*\.[a-z]([a-z0-9-]{0,61}[a-z0-9])?$/;

/**
 * Lowercases `handle`, substituting `handle.invalid` for anything that is not a
 * syntactically valid handle.
 *
 * Habitat already returns `handle.invalid` for handles it could not verify, so
 * this is defensive rather than load-bearing.
 */
export function normalizeHandle(handle: string): string {
  const lowered = handle.toLowerCase();

  if (lowered.length > MAX_HANDLE_LENGTH) {
    return HANDLE_INVALID;
  }

  return HANDLE_REGEX.test(lowered) ? lowered : HANDLE_INVALID;
}
