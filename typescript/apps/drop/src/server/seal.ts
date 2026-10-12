// AES-GCM sealing for credentials at rest. OAuth sessions (refresh tokens and
// DPoP private keys) are bearer credentials for the member or org they belong
// to, so they're never written to D1 in the clear: anyone with a database
// backup but not DROP_CREDENTIALS_KEY gets nothing usable.
//
// The key is 32 random bytes, base64-encoded (`openssl rand -base64 32`).
// A sealed value is the 12-byte IV followed by the ciphertext.

const IV_BYTES = 12;

const keys = new Map<string, Promise<CryptoKey>>();

function importKey(secret: string): Promise<CryptoKey> {
  let key = keys.get(secret);
  if (!key) {
    const raw = Uint8Array.from(atob(secret), (c) => c.charCodeAt(0));
    if (raw.length !== 32) {
      throw new Error("DROP_CREDENTIALS_KEY must be 32 base64-encoded bytes");
    }
    key = crypto.subtle.importKey("raw", raw, "AES-GCM", false, [
      "encrypt",
      "decrypt",
    ]);
    keys.set(secret, key);
  }
  return key;
}

export async function seal(
  secret: string,
  value: unknown,
): Promise<Uint8Array> {
  const iv = crypto.getRandomValues(new Uint8Array(IV_BYTES));
  const plaintext = new TextEncoder().encode(JSON.stringify(value));
  const ciphertext = await crypto.subtle.encrypt(
    { name: "AES-GCM", iv },
    await importKey(secret),
    plaintext,
  );
  const out = new Uint8Array(IV_BYTES + ciphertext.byteLength);
  out.set(iv);
  out.set(new Uint8Array(ciphertext), IV_BYTES);
  return out;
}

export async function unseal<T>(
  secret: string,
  sealed: Uint8Array,
): Promise<T> {
  const plaintext = await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: sealed.slice(0, IV_BYTES) },
    await importKey(secret),
    sealed.slice(IV_BYTES),
  );
  return JSON.parse(new TextDecoder().decode(plaintext)) as T;
}
