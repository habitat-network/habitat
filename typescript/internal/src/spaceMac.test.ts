import {
  P256Keypair,
  P256_JWT_ALG,
  Secp256k1Keypair,
  formatDidKey,
} from "@atproto/crypto";
import type { Commit } from "@atproto/space";
import { describe, expect, it } from "vitest";
import {
  COMMIT_MAC_PROFILE,
  RECORD_MAC_PROFILE,
  createCommitMacHeaders,
  decapsulate,
  encapsulate,
  formatMacRequest,
  parseMacRequest,
  verifyCommit,
} from "./spaceMac";

const hex = (s: string) =>
  new Uint8Array(s.match(/../g)!.map((b) => parseInt(b, 16)));
const toHex = (b: Uint8Array) =>
  [...b].map((x) => x.toString(16).padStart(2, "0")).join("");

describe("HPKE secret export", () => {
  it("matches the reference vector", async () => {
    // Computed by the upstream implementation and checked against Go's
    // crypto/hpke: DHKEM(P-256), HKDF-SHA256, export-only, info "atproto-mac".
    const enc = hex(
      "04d804370b7e24b94749eb1dc8df6d4d4a5d75f9effad01739ebcad5c54a40d57aaa8b4190fc124dbde2e4f1e1d1b012a3bc4038157dc29b55533a932306d8d38d",
    );
    const skRm = hex(
      "62c3868357a464f8461d03aa0182c7cebcde841036aea7230ddc7339f1088346",
    );
    const recipient = await P256Keypair.import(skRm);
    const keyShare = formatDidKey(P256_JWT_ALG, enc);
    expect(toHex(decapsulate(recipient, keyShare))).toBe(
      "2060e0558d1b34c10086bc137f849bb79ba59fcdc30544418e35c9cd301f0d0d",
    );
  });

  it("derives the secret the author decapsulates, fresh per request", async () => {
    const author = await P256Keypair.create();
    const a = await encapsulate(author.did());
    const b = await encapsulate(author.did());
    expect(decapsulate(author, a.keyShare)).toEqual(a.sharedSecret);
    expect(a.keyShare).not.toBe(b.keyShare);
  });

  it("rejects a key that is not P-256", async () => {
    const k256 = await Secp256k1Keypair.create();
    await expect(encapsulate(k256.did())).rejects.toThrow(/P-256/);
  });
});

describe("MAC requests", () => {
  it("round-trips through the host's parser", () => {
    const value = formatMacRequest(COMMIT_MAC_PROFILE, "did:key:zExample");
    expect(value).toBe(
      'mac=("atproto-space-commit";sf);keyid="did:key:zExample";tag="atproto-space-commit"',
    );
    const parsed = parseMacRequest(value, COMMIT_MAC_PROFILE)!;
    expect(parsed.label).toBe("mac");
    expect(parsed.keyShare).toBe("did:key:zExample");
    expect(parseMacRequest(value, RECORD_MAC_PROFILE)).toBeUndefined();
    expect(parseMacRequest(null, COMMIT_MAC_PROFILE)).toBeUndefined();
  });
});

describe("commit MACs", () => {
  const commit: Commit = {
    ver: 1,
    space: "at://did:plc:owner/space/com.example.type/key",
    repo: "did:plc:alice",
    rev: "3mwcj3b6uak2f",
    data: new Uint8Array(32).fill(7),
  } as unknown as Commit;

  const exchange = async () => {
    const author = await P256Keypair.create();
    const enc = await encapsulate(author.did());
    const request = parseMacRequest(
      formatMacRequest(COMMIT_MAC_PROFILE, enc.keyShare),
      COMMIT_MAC_PROFILE,
    )!;
    const headers = createCommitMacHeaders(commit, {
      ...request,
      sharedSecret: decapsulate(author, enc.keyShare),
    });
    return { enc, headers: new Headers(headers) };
  };
  const params = { space: commit.space, repo: commit.repo };

  it("verifies a host's response", async () => {
    const { enc, headers } = await exchange();
    expect(headers.get("atproto-space-commit")).toContain(
      'rev="3mwcj3b6uak2f"',
    );
    expect(verifyCommit(commit, { ...enc, headers, ...params })).toBe(true);
  });

  it("rejects a different commit, secret or missing headers", async () => {
    const { enc, headers } = await exchange();
    expect(
      verifyCommit(
        { ...commit, rev: "3mwcj3b6uak2g" },
        { ...enc, headers, ...params },
      ),
    ).toBe(false);
    const wrong = { ...enc, sharedSecret: new Uint8Array(32) };
    expect(verifyCommit(commit, { ...wrong, headers, ...params })).toBe(false);
    expect(
      verifyCommit(commit, { ...enc, headers: new Headers(), ...params }),
    ).toBe(false);
  });
});
