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
const id = 'network.habitat.repo.listRecords'

export type QueryParams = {}

export interface InputSchema {
  subjects: string[]
  /** Filter by specific lexicons */
  collection: string
  /** Allow getting records that are strictly newer or updated since a certain time. */
  since?: string
  /** [UNIMPLEMENTED] The number of records to return. (Default value should be 50 to be consistent with atproto API). */
  limit?: number
  /** [UNIMPLEMENTED] Cursor of the returned list. */
  cursor?: string
}

export interface OutputSchema {
  cursor?: string
  records: Record[]
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

export interface Record {
  $type?: 'network.habitat.repo.listRecords#record'
  /** URI reference to the record, formatted as a habitat-uri. */
  uri: string
  cid: string
  value: { [_ in string]: unknown }
}

const hashRecord = 'record'

export function isRecord<V>(v: V) {
  return is$typed(v, id, hashRecord)
}

export function validateRecord<V>(v: V) {
  return validate<Record & V>(v, id, hashRecord)
}
