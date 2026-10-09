// pear's network.habitat.repo.uploadBlob rejects bodies over 500 KiB
// (internal/pearserver/space_upload_blob.go), so Drop checks the same limit
// up front instead of uploading and failing.
export const MAX_FILE_BYTES = 500 * 1024;

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value < 10 ? value.toFixed(1) : Math.round(value)} ${units[unit]}`;
}
