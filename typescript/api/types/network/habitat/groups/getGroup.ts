/**
 * GENERATED CODE - DO NOT MODIFY
 */
import { HeadersMap, XRPCError } from '@atproto/xrpc'
import { type ValidationResult, BlobRef } from '@atproto/lexicon'
import { CID } from 'multiformats/cid'
import { validate as _validate } from '../../../../lexicons.js'
import {
  type $Typed,
  is$typed as _is$typed,
  type OmitKey,
} from '../../../../util.js'
import type * as NetworkHabitatGroupsDefs from './defs.js'

const is$typed = _is$typed,
  validate = _validate
const id = 'network.habitat.groups.getGroup'

export type QueryParams = {
  /** URI of the group-space. */
  group: string
}
export type InputSchema = undefined
export type OutputSchema = NetworkHabitatGroupsDefs.GroupView

export interface CallOptions {
  signal?: AbortSignal
  headers?: HeadersMap
}

export interface Response {
  success: boolean
  headers: HeadersMap
  data: OutputSchema
}

export class GroupNotFoundError extends XRPCError {
  constructor(src: XRPCError) {
    super(src.status, src.error, src.message, src.headers, { cause: src })
  }
}

export function toKnownErr(e: any) {
  if (e instanceof XRPCError) {
    if (e.error === 'GroupNotFound') return new GroupNotFoundError(e)
  }

  return e
}
