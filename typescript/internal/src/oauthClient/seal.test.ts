import { describe, expect, it } from "vitest";
import { seal, unseal } from "./seal";

const KEY = "dGVzdC1vbmx5LWNyZWRlbnRpYWxzLWtleS0zMmJ5dGU=";
const OTHER_KEY = btoa("another-test-key-of-32-bytes!!!!!");

describe("seal", () => {
  it("round-trips a value without leaking it", async () => {
    const sealed = await seal(KEY, { token: "secret-refresh-token" });
    expect(new TextDecoder().decode(sealed)).not.toContain("secret");
    expect(await unseal(KEY, sealed)).toEqual({
      token: "secret-refresh-token",
    });
  });

  it("uses a fresh nonce each time", async () => {
    const a = await seal(KEY, "x");
    const b = await seal(KEY, "x");
    expect(a).not.toEqual(b);
  });

  it("reads rows written by the earlier hand-rolled WebCrypto code", async () => {
    // The old format: 12-byte IV followed by AES-GCM ciphertext‖tag.
    const raw = Uint8Array.from(atob(KEY), (c) => c.charCodeAt(0));
    const key = await crypto.subtle.importKey("raw", raw, "AES-GCM", false, [
      "encrypt",
    ]);
    const iv = crypto.getRandomValues(new Uint8Array(12));
    const ct = new Uint8Array(
      await crypto.subtle.encrypt(
        { name: "AES-GCM", iv },
        key,
        new TextEncoder().encode(JSON.stringify({ a: 1 })),
      ),
    );
    const legacy = new Uint8Array(12 + ct.length);
    legacy.set(iv);
    legacy.set(ct, 12);
    expect(await unseal(KEY, legacy)).toEqual({ a: 1 });
  });

  it("rejects the wrong key and tampered data", async () => {
    const sealed = await seal(KEY, "x");
    await expect(unseal(OTHER_KEY, sealed)).rejects.toThrow();
    sealed[sealed.length - 1] ^= 1;
    await expect(unseal(KEY, sealed)).rejects.toThrow();
  });

  it("rejects a key that is not 32 bytes", async () => {
    await expect(seal(btoa("short"), "x")).rejects.toThrow();
  });
});
