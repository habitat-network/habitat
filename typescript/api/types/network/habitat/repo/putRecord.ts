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

export interface DidGrantee {
  $type?: 'network.habitat.repo.putRecord#didGrantee'
  did: string
}

const hashDidGrantee = 'didGrantee'

export function isDidGrantee<V>(v: V) {
  return is$typed(v, id, hashDidGrantee)
}

export function validateDidGrantee<V>(v: V) {
  return validate<DidGrantee & V>(v, id, hashDidGrantee)
}

export interface CliqueRef {
  $type?: 'network.habitat.repo.putRecord#cliqueRef'
  /** A habitat-uri pointing to a clique owner (habitat://<did>/<collection>/<rkey>) */
  uri: string
}

const hashCliqueRef = 'cliqueRef'

export function isCliqueRef<V>(v: V) {
  return is$typed(v, id, hashCliqueRef)
}

export function validateCliqueRef<V>(v: V) {
  return validate<CliqueRef & V>(v, id, hashCliqueRef)
}

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
  grantees?: ($Typed<DidGrantee> | $Typed<CliqueRef> | { $type: string })[]
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
