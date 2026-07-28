/**
 * GENERATED CODE - DO NOT MODIFY
 */
import { type ValidationResult, BlobRef } from '@atproto/lexicon'
import { CID } from 'multiformats/cid'
import { validate as _validate } from '../../../../lexicons.js'
import {
  type $Typed,
  is$typed as _is$typed,
  type OmitKey,
} from '../../../../util.js'

const is$typed = _is$typed,
  validate = _validate
const id = 'network.habitat.space.defs'

/** A signed commit over the current state of a permissioned repo. */
export interface SignedCommit {
  $type?: 'network.habitat.space.defs#signedCommit'
  /** Commit format version, currently 1. Corresponds to the version in the ctx protocol tag (atproto-space-v1). */
  ver: number
  /** sha256 digest of the LtHash state (32 bytes). */
  hash: Uint8Array
  /** Per-signature input keying material (32 random bytes) */
  ikm: Uint8Array
  /** Signature over ctx (space, author DID, rev, ikm) by the user's atproto signing key. Does not cover the repo hash. */
  sig: Uint8Array
  /** HMAC-SHA256 over hash, keyed by HKDF-SHA256(ikm, info=ctx). Binds the repo hash to this commit's context. */
  mac: Uint8Array
  /** Commit revision (TID), also bound into ctx. */
  rev: string
}

const hashSignedCommit = 'signedCommit'

export function isSignedCommit<V>(v: V) {
  return is$typed(v, id, hashSignedCommit)
}

export function validateSignedCommit<V>(v: V) {
  return validate<SignedCommit & V>(v, id, hashSignedCommit)
}
