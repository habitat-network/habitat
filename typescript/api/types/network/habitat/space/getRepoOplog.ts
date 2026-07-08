/**
 * GENERATED CODE - DO NOT MODIFY
 */
import { type HeadersMap, XRPCError } from '@atproto/xrpc'
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
const id = 'network.habitat.space.getRepoOplog'

export type QueryParams = {
  /** Reference to the space. */
  space: string
  /** The DID of the member whose records to track. */
  repo: string
  /** Return records with revisions after this value (exclusive). */
  since?: string
  /** Maximum number of records to return. */
  limit?: number
}
export type InputSchema = undefined

export interface OutputSchema {
  records: Record[]
  /** The revision of the last returned record. Use as `since` in the next poll. */
  cursor?: string
}

export interface CallOptions {
  signal?: AbortSignal
  headers?: HeadersMap
}

export interface Response {
  success: boolean
  headers: HeadersMap
  data: OutputSchema
}

export class SpaceNotFoundError extends XRPCError {
  constructor(src: XRPCError) {
    super(src.status, src.error, src.message, src.headers, { cause: src })
  }
}

export function toKnownErr(e: any) {
  if (e instanceof XRPCError) {
    if (e.error === 'SpaceNotFound') return new SpaceNotFoundError(e)
  }

  return e
}

export interface Record {
  $type?: 'network.habitat.space.getRepoOplog#record'
  /** Revision (TID) of this record. */
  rev: string
  collection: string
  rkey: string
  cid?: string
  /** The record value. */
  value: { [_ in string]: unknown }
}

const hashRecord = 'record'

export function isRecord<V>(v: V) {
  return is$typed(v, id, hashRecord)
}

export function validateRecord<V>(v: V) {
  return validate<Record & V>(v, id, hashRecord)
}
