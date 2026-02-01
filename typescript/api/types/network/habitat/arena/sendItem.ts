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
const id = 'network.habitat.arena.sendItem'

export type QueryParams = {}

export interface InputSchema {
  /** The URI for the item to send to the arena, formatted as a habitat-uri. */
  item: string
  /** The ID of the arena to send the item to, formatted as a habitat-uri. */
  arenaID: string
}

export interface OutputSchema {
  /** Result status of the send operation, e.g., 'success' or 'error'. */
  status?: string
  /** Optional message providing additional information about the operation. */
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
