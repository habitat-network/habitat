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
  /** The DID to grant permission to (URL parameter). */
  recipient: string
  /** The NSID of the record collection that the update is for. */
  collection: string
  /** The record key which was updated. */
  rkey: string
  /** The reason, or metadata, about this notification. The clique uri if the update was due to shared clique membership. */
  reason?: string
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
}

export function toKnownErr(e: any) {
  return e
}
