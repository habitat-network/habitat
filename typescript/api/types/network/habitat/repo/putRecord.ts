/**
 * GENERATED CODE - DO NOT MODIFY
 */
import { HeadersMap, XRPCError } from '@atproto/xrpc'
import { type ValidationResult, BlobRef } from '@atproto/lexicon'
import { CID } from 'multiformats/cid'
import { validate as _validate } from '../../../../lexicons'
import {
  type $Typed,
  is$typed as _is$typed,
  type OmitKey,
} from '../../../../util'

const is$typed = _is$typed,
  validate = _validate
const id = 'network.habitat.repo.putRecord'

export type QueryParams = {}

export interface InputSchema {
  /** The handle or DID of the repo (aka, current account). */
  repo: string
  /** The NSID of the record collection. */
  collection: string
  /** The Record Key. */
  rkey: string
  /** Can be set to 'false' to skip Lexicon schema validation of record data, 'true' to require it, or leave unset to validate only for known Lexicons. */
  validate?: boolean
  /** The record to write. */
  record: { [_ in string]: unknown }
  grantees?: ($Typed<DidGrantee> | $Typed<ArenaGrantee> | { $type: string })[]
  /** Whether to create an arena, allowing all grantees to aggregate records under this arena. */
  createArena?: boolean
}

export interface OutputSchema {
  /** The habitat-uri of the put-ed object. */
  uri: string
  validationStatus?: 'valid' | 'unknown' | (string & {})
}

export interface CallOptions {
  signal?: AbortSignal
  headers?: HeadersMap
  qp?: QueryParams
  encoding?: 'application/json'
}

export interface Response {
  success: boolean
  headers: HeadersMap
  data: OutputSchema
}

export function toKnownErr(e: any) {
  return e
}

export type DidGrantee = string
/** The habitat-uri of the arena to delegate permissions to. */
export type ArenaGrantee = string
