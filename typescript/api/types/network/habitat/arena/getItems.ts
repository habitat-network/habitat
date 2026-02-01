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
const id = 'network.habitat.arena.getItems'

export type QueryParams = {
  /** The ID of the arena to retrieve items from, formatted as a habitat-uri. */
  arenaID: string
}
export type InputSchema = undefined

export interface OutputSchema {
  /** Token providing proof that the caller can read the record, verifiable by the repos hosting the arena's items. */
  allowToken: string
  /** The list of items present in the arena, referenced by habitat-uris. */
  items: Record[]
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
