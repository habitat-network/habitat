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
const id = 'network.habitat.internal.notifyOfUpdate'

export type QueryParams = {}

export interface InputSchema {
  /** The NSID of the record collection that the update is for. */
  collection: string
  /** The DID to grant permission to (URL parameter). */
  did: string
}

export interface OutputSchema {
  /** Result status of the permission grant, e.g., 'success' or 'error'. */
  status?: string
  /** Optional message providing more details about the operation. */
  message?: string
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
