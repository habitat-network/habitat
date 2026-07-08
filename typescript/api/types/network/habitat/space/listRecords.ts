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
const id = 'network.habitat.space.listRecords'

export type QueryParams = {
  /** Reference to the space. */
  space: string
  /** The DID of the member whose repo to read from. If omitted, defaults to the authenticated user. */
  repo?: string
  /** The NSID of the record type. */
  collection?: string
  limit?: number
  cursor?: string
  /** Flag to reverse the order of the returned records. */
  reverse?: boolean
}
export type InputSchema = undefined

export interface OutputSchema {
  cursor?: string
  records: Record[]
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

export function toKnownErr(e: any) {
  return e
}

export interface Record {
  $type?: 'network.habitat.space.listRecords#record'
  collection: string
  rkey: string
  cid: string
  updatedAt?: string
  value?: { [_ in string]: unknown }
}

const hashRecord = 'record'

export function isRecord<V>(v: V) {
  return is$typed(v, id, hashRecord)
}

export function validateRecord<V>(v: V) {
  return validate<Record & V>(v, id, hashRecord)
}
