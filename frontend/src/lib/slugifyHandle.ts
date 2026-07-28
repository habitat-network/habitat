/**
 * Derives a valid handle-subdomain candidate from a display name by
 * lowercasing and stripping every character outside a-z0-9, then
 * truncating to the backend's max handle length (see
 * internal/hive/hive.go's `^[a-zA-Z0-9]{1,50}$` pattern).
 */
export function slugifyHandle(input: string): string {
  return input
    .toLowerCase()
    .replace(/[^a-z0-9]/g, "")
    .slice(0, 50);
}
