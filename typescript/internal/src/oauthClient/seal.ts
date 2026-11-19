import { Effect, Layer } from "effect";
import { Aead, SecureRandom } from "effect/crypto";

// AES-256-GCM sealing for credentials at rest, on Effect's Aead and
// SecureRandom services. OAuth sessions (refresh tokens and DPoP private keys)
// are bearer credentials for the member or org they belong to, so they're
// never written to a database in the clear: anyone with a backup but not the
// credentials key gets nothing usable.
//
// The key is 32 random bytes, base64-encoded (`openssl rand -base64 32`).
// A sealed value is the 12-byte nonce followed by ciphertext‖tag, the same
// layout the apps' earlier hand-rolled WebCrypto code wrote, so existing rows
// stay readable.

const NONCE_BYTES = 12;

const layer = Aead.layerWebCrypto.pipe(
  Layer.provideMerge(SecureRandom.layerWebCrypto),
);

// importKey decodes and validates the base64 secret into an Aead key.
const importKey = (secret: string) =>
  Effect.gen(function* () {
    const raw = Uint8Array.from(atob(secret), (c) => c.charCodeAt(0));
    if (raw.length !== 32) {
      return yield* Effect.die(
        new Error("credentials key must be 32 base64-encoded bytes"),
      );
    }
    return yield* Aead.key(Aead.aes256Gcm, raw);
  });

const sealEffect = (secret: string, value: unknown) =>
  Effect.gen(function* () {
    const random = yield* SecureRandom.SecureRandom;
    const aead = yield* Aead.Aead;
    const key = yield* importKey(secret);
    const nonce = yield* random.randomBytes(NONCE_BYTES);
    const ciphertext = yield* aead.encrypt(
      key,
      new TextEncoder().encode(JSON.stringify(value)),
      { nonce },
    );
    const out = new Uint8Array(NONCE_BYTES + ciphertext.length);
    out.set(nonce);
    out.set(ciphertext, NONCE_BYTES);
    return out;
  });

const unsealEffect = <T>(secret: string, sealed: Uint8Array) =>
  Effect.gen(function* () {
    const aead = yield* Aead.Aead;
    const key = yield* importKey(secret);
    const plaintext = yield* aead.decrypt(key, sealed.slice(NONCE_BYTES), {
      nonce: sealed.slice(0, NONCE_BYTES),
    });
    return JSON.parse(new TextDecoder().decode(plaintext)) as T;
  });

export function seal(secret: string, value: unknown): Promise<Uint8Array> {
  return Effect.runPromise(
    sealEffect(secret, value).pipe(Effect.provide(layer)),
  );
}

export function unseal<T>(secret: string, sealed: Uint8Array): Promise<T> {
  return Effect.runPromise(
    unsealEffect<T>(secret, sealed).pipe(Effect.provide(layer)),
  );
}
