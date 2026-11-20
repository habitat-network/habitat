// Space MACs: HPKE secret export (RFC 9180 §6.2) for the per-request shared
// secret, and RFC 9421 HTTP message signatures using hmac-sha256 to
// authenticate commits and records with it. Mirrors the spaces protocol alpha
// (permissioned-data-alpha); the @atproto/space release this repo depends on
// still implements the earlier Want-Mac scheme, so the new one lives here.
import {
  P256Keypair,
  P256_JWT_ALG,
  hmacSha256,
  parseDidKey,
} from "@atproto/crypto";
import type { SpaceCommit as Commit } from "@atproto/space";
import {
  ByteSequence,
  type BareItem,
  type Dictionary,
  type InnerList,
  type Item,
  isByteSequence,
  isInnerList,
  parseDictionary,
  serializeDictionary,
  serializeInnerList,
  serializeItem,
} from "structured-headers";

export const COMMIT_MAC_PROFILE = "atproto-space-commit";
export const RECORD_MAC_PROFILE = "atproto-space-record";
export type MacProfile = typeof COMMIT_MAC_PROFILE | typeof RECORD_MAC_PROFILE;

export const ACCEPT_SIGNATURE_HEADER = "accept-signature";
/** The error a repo host answers a missing or unusable MAC request with. */
export const INVALID_SPACE_MAC = "InvalidSpaceMac";

const MAC_ALG = "hmac-sha256";
const MAC_LABEL = "mac";
const MAC_REQUEST_PARAMS = ["keyid", "tag", "alg"];

export class SpaceMacError extends Error {}

// ---- bytes ----

const utf8 = (s: string) => new TextEncoder().encode(s);

const concat = (parts: Uint8Array[]): Uint8Array => {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let at = 0;
  for (const p of parts) {
    out.set(p, at);
    at += p.length;
  }
  return out;
};

const uint16be = (v: number) => new Uint8Array([v >>> 8, v & 0xff]);

const timingSafeEqual = (a: Uint8Array, b: Uint8Array): boolean => {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a[i]! ^ b[i]!;
  return diff === 0;
};

// Structured field byte sequences are padded standard base64 (RFC 9651 §4.1.8).
const toPaddedBase64 = (bytes: Uint8Array): string => {
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin);
};

const fromBase64 = (b64: string): Uint8Array => {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
};

// ---- HPKE secret export ----
//
// DHKEM(P-256, HKDF-SHA256) with HKDF-SHA256 and the Export-only AEAD, spelled
// out for that one suite so nothing is encrypted and no HPKE library is needed.

export interface Encapsulation {
  /** HPKE's `enc`: the ephemeral public key as a did:key, sent as `keyid`. */
  keyShare: string;
  sharedSecret: Uint8Array;
}

const HPKE_INFO = utf8("atproto-mac");
const HPKE_VERSION = utf8("HPKE-v1");
const KEM_ID = concat([utf8("KEM"), uint16be(0x0010)]);
const SUITE_ID = concat([
  utf8("HPKE"),
  uint16be(0x0010),
  uint16be(0x0001),
  uint16be(0xffff),
]);
const MODE_BASE = new Uint8Array([0x00]);
const EMPTY = new Uint8Array(0);

// HKDF-Extract: HMAC keyed by the salt (32 zero bytes when empty).
const hkdfExtract = (salt: Uint8Array, ikm: Uint8Array) =>
  hmacSha256(salt.length ? salt : new Uint8Array(32), ikm);

// HKDF-Expand for outputs of at most one hash block (32 bytes).
const hkdfExpand = (prk: Uint8Array, info: Uint8Array, length: number) =>
  hmacSha256(prk, concat([info, new Uint8Array([1])])).slice(0, length);

const labeledExtract = (
  id: Uint8Array,
  salt: Uint8Array,
  label: string,
  ikm: Uint8Array,
) => hkdfExtract(salt, concat([HPKE_VERSION, id, utf8(label), ikm]));

const labeledExpand = (
  id: Uint8Array,
  prk: Uint8Array,
  label: string,
  info: Uint8Array,
  length: number,
) =>
  hkdfExpand(
    prk,
    concat([uint16be(length), HPKE_VERSION, id, utf8(label), info]),
    length,
  );

// SetupBase{S,R} followed by Export("", 32), given the ECDH result `dh`.
const deriveSharedSecret = (
  dh: Uint8Array,
  enc: Uint8Array,
  recipientKey: Uint8Array,
): Uint8Array => {
  const eaePrk = labeledExtract(KEM_ID, EMPTY, "eae_prk", dh);
  const kemSecret = labeledExpand(
    KEM_ID,
    eaePrk,
    "shared_secret",
    concat([enc, recipientKey]),
    32,
  );
  const pskIdHash = labeledExtract(SUITE_ID, EMPTY, "psk_id_hash", EMPTY);
  const infoHash = labeledExtract(SUITE_ID, EMPTY, "info_hash", HPKE_INFO);
  const context = concat([MODE_BASE, pskIdHash, infoHash]);
  const secret = labeledExtract(SUITE_ID, kemSecret, "secret", EMPTY);
  const exporterSecret = labeledExpand(SUITE_ID, secret, "exp", context, 32);
  return labeledExpand(SUITE_ID, exporterSecret, "sec", EMPTY, 32);
};

// The uncompressed public key (0x04 || x || y) of a P-256 did:key. HPKE
// encodes keys uncompressed even though did:key uses the compressed form.
const p256KeyBytes = (didKey: string): Uint8Array => {
  let parsed;
  try {
    parsed = parseDidKey(didKey);
  } catch (cause) {
    throw new SpaceMacError("invalid KEM key", { cause });
  }
  if (parsed.jwtAlg !== P256_JWT_ALG) {
    throw new SpaceMacError("KEM key must be a P-256 did:key");
  }
  return parsed.keyBytes;
};

/**
 * Syncer side: a fresh ephemeral keypair and the secret it shares with the
 * author's `#atproto_kem` key. Use a new one for every request.
 */
export const encapsulate = async (
  authorKemKey: string,
): Promise<Encapsulation> => {
  const recipient = p256KeyBytes(authorKemKey);
  const ephemeral = await P256Keypair.create();
  const keyShare = ephemeral.did();
  return {
    keyShare,
    sharedSecret: deriveSharedSecret(
      ephemeral.ecdh(recipient),
      p256KeyBytes(keyShare),
      recipient,
    ),
  };
};

/** Host side: the same secret, from the syncer's `keyShare`. */
export const decapsulate = (kem: P256Keypair, keyShare: string): Uint8Array => {
  const enc = p256KeyBytes(keyShare);
  return deriveSharedSecret(kem.ecdh(enc), enc, p256KeyBytes(kem.did()));
};

// ---- RFC 9421 plumbing ----

const macComponents = (profile: MacProfile): Item[] => [
  [profile, new Map([["sf", true]])],
];

const formatComponents = (components: Item[]) =>
  serializeInnerList([components, new Map()]);

const signatureBase = (
  components: Item[],
  values: string[],
  signatureParams: string,
): Uint8Array => {
  const lines = components.map(
    (c, i) => `${serializeItem(c)}: ${values[i]!.trim()}`,
  );
  lines.push(`"@signature-params": ${signatureParams}`);
  return utf8(lines.join("\n"));
};

const parseField = (
  value: string | null | undefined,
  name: string,
): Dictionary => {
  if (typeof value !== "string")
    throw new SpaceMacError(`missing or malformed ${name} header`);
  try {
    return parseDictionary(value);
  } catch (cause) {
    throw new SpaceMacError(`malformed ${name} header`, { cause });
  }
};

const membersWithTag = (members: Dictionary, tag: string) => {
  const tagged: { label: string; params: InnerList }[] = [];
  for (const [label, member] of members) {
    if (isInnerList(member) && member[1].get("tag") === tag)
      tagged.push({ label, params: member });
  }
  return tagged;
};

const bareItem = (fields: Dictionary, key: string): BareItem | undefined => {
  const member = fields.get(key);
  return member && !isInnerList(member) ? member[0] : undefined;
};

// ---- requests ----

/**
 * The `Accept-Signature` value asking for a MAC keyed by the secret behind
 * `keyShare`, e.g.
 * `mac=("atproto-space-commit";sf);keyid="did:key:z…";tag="atproto-space-commit"`.
 */
export const formatMacRequest = (
  profile: MacProfile,
  keyShare: string,
): string =>
  `${MAC_LABEL}=${serializeInnerList([
    macComponents(profile),
    new Map<string, BareItem>([
      ["keyid", keyShare],
      ["tag", profile],
    ]),
  ])}`;

export interface MacRequest {
  label: string;
  keyShare: string;
  /** The requested signature parameters, serialized; the host MACs them as given. */
  signatureParams: string;
}

/**
 * The MAC requested for `profile` in an `Accept-Signature` header, or
 * undefined when none is. Throws SpaceMacError for one a host cannot fulfil.
 */
export const parseMacRequest = (
  value: string | null | undefined,
  profile: MacProfile,
): MacRequest | undefined => {
  if (value === null || value === undefined) return undefined;
  const tagged = membersWithTag(parseField(value, "Accept-Signature"), profile);
  if (tagged.length === 0) return undefined;
  if (tagged.length > 1)
    throw new SpaceMacError(
      `more than one MAC requested with tag "${profile}"`,
    );
  const { label, params } = tagged[0]!;
  const [components, parameters] = params;
  const expected = formatComponents(macComponents(profile));
  if (formatComponents(components) !== expected)
    throw new SpaceMacError(`MAC must cover exactly ${expected}`);
  for (const name of parameters.keys()) {
    if (!MAC_REQUEST_PARAMS.includes(name))
      throw new SpaceMacError(`unsupported MAC parameter: ${name}`);
  }
  if (parameters.has("alg") && parameters.get("alg") !== MAC_ALG)
    throw new SpaceMacError(`MAC algorithm must be ${MAC_ALG}`);
  const keyShare = parameters.get("keyid");
  if (typeof keyShare !== "string" || !keyShare.startsWith("did:"))
    throw new SpaceMacError("missing or malformed keyid");
  return { label, keyShare, signatureParams: serializeInnerList(params) };
};

// ---- responses (host side, used by the fake network in tests) ----

export type CreateMacParams = MacRequest & { sharedSecret: Uint8Array };

const createMacHeaders = (
  profile: MacProfile,
  covered: string,
  { label, signatureParams, sharedSecret }: CreateMacParams,
): Record<string, string> => {
  const mac = hmacSha256(
    sharedSecret,
    signatureBase(macComponents(profile), [covered], signatureParams),
  );
  return {
    [profile]: covered,
    "signature-input": `${label}=${signatureParams}`,
    signature: `${label}=:${toPaddedBase64(mac)}:`,
  };
};

export const createCommitMacHeaders = (
  commit: Commit,
  params: CreateMacParams,
): Record<string, string> =>
  createMacHeaders(
    COMMIT_MAC_PROFILE,
    serializeDictionary(
      new Map<string, Item>([
        ["ver", [commit.ver, new Map()]],
        ["repo", [commit.repo, new Map()]],
        ["space", [commit.space, new Map()]],
        ["rev", [commit.rev, new Map()]],
        ["data", [new ByteSequence(toPaddedBase64(commit.data)), new Map()]],
      ]),
    ),
    params,
  );

// ---- verification (syncer side) ----

/** A response's MAC: the request's secret and key share, and the response headers. */
export interface MacExchange extends Encapsulation {
  headers: Headers;
}

// Checks the MAC requested for `profile`, returning the fields of the header it covers.
const verifyMac = (
  profile: MacProfile,
  { keyShare, sharedSecret, headers }: MacExchange,
): Dictionary => {
  const input = headers.get("signature-input");
  const signatureField = headers.get("signature");
  let inputs: Dictionary, signatures: Dictionary;
  try {
    if (input === null || signatureField === null) throw new Error("absent");
    inputs = parseDictionary(input);
    signatures = parseDictionary(signatureField);
  } catch (cause) {
    throw new SpaceMacError("missing or malformed signature headers", {
      cause,
    });
  }
  const tagged = membersWithTag(inputs, profile);
  if (tagged.length !== 1)
    throw new SpaceMacError(`expected one signature with tag "${profile}"`);
  const { label, params } = tagged[0]!;
  const sig = signatures.get(label);
  if (!sig || isInnerList(sig) || !isByteSequence(sig[0]) || sig[1].size !== 0)
    throw new SpaceMacError(`malformed signature with tag "${profile}"`);
  const signature = fromBase64(sig[0].toBase64());

  const components = macComponents(profile);
  const [covered, parameters] = params;
  if (formatComponents(covered) !== formatComponents(components))
    throw new SpaceMacError("MAC covers the wrong components");
  if (parameters.has("alg") && parameters.get("alg") !== MAC_ALG)
    throw new SpaceMacError(`MAC algorithm must be ${MAC_ALG}`);
  if (parameters.has("keyid") && parameters.get("keyid") !== keyShare)
    throw new SpaceMacError("MAC keyid does not match the key share");

  const fields = parseField(headers.get(profile), profile);
  // The `sf` flag covers the header as re-serialized from its parsed form.
  const base = signatureBase(
    components,
    [serializeDictionary(fields)],
    serializeInnerList(params),
  );
  if (!timingSafeEqual(hmacSha256(sharedSecret, base), signature))
    throw new SpaceMacError("MAC does not match");
  return fields;
};

/**
 * Verifies the response's MAC and that the `Atproto-Space-Commit` header it
 * covers describes `commit`. Throws SpaceMacError otherwise.
 */
export const verifyCommitMac = (commit: Commit, exchange: MacExchange) => {
  const fields = verifyMac(COMMIT_MAC_PROFILE, exchange);
  const data = bareItem(fields, "data");
  const matches =
    bareItem(fields, "ver") === commit.ver &&
    bareItem(fields, "repo") === commit.repo &&
    bareItem(fields, "space") === commit.space &&
    bareItem(fields, "rev") === commit.rev &&
    data !== undefined &&
    isByteSequence(data) &&
    timingSafeEqual(fromBase64(data.toBase64()), commit.data);
  if (!matches) throw new SpaceMacError("commit does not match its MAC");
};

/**
 * Whether `commit` is the one `repo` published in `space`, as authenticated by
 * the response's MAC. Once this passes, `commit.data` is trusted as the
 * author's claim about their repo.
 */
export const verifyCommit = (
  commit: Commit,
  params: MacExchange & { space: string; repo: string },
): boolean => {
  if (commit.ver !== 1) return false;
  if (commit.repo !== params.repo || commit.space !== params.space)
    return false;
  try {
    verifyCommitMac(commit, params);
    return true;
  } catch (error) {
    if (error instanceof SpaceMacError) return false;
    throw error;
  }
};
