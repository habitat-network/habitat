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
import type * as NetworkHabitatSimplespaceDefs from './defs.js'

const is$typed = _is$typed,
  validate = _validate
const id = 'network.habitat.simplespace.createSpace'

export type QueryParams = {}

export interface InputSchema {
  /** The DID of the space. */
  did: string
  /** The NSID of the space type, describing the modality of the space (e.g. app.bsky.group, app.bsky.personal). */
  type: string
  /** The space key. Used to differentiate multiple spaces of the same type under the same owner. If not provided, one will be auto-generated (TID). */
  skey?: string
  config?: NetworkHabitatSimplespaceDefs.SpaceConfig
}

export interface OutputSchema {
  /** URI of the created space. */
  uri: string
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

export class SpaceAlreadyExistsError extends XRPCError {
  constructor(src: XRPCError) {
    super(src.status, src.error, src.message, src.headers, { cause: src })
  }
}

export class InvalidTypeError extends XRPCError {
  constructor(src: XRPCError) {
    super(src.status, src.error, src.message, src.headers, { cause: src })
  }
}

export function toKnownErr(e: any) {
  if (e instanceof XRPCError) {
    if (e.error === 'SpaceAlreadyExists') return new SpaceAlreadyExistsError(e)
    if (e.error === 'InvalidType') return new InvalidTypeError(e)
  }

  return e
}
