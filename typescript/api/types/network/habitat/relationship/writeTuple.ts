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
import type * as NetworkHabitatRelationshipDefs from './defs.js'

const is$typed = _is$typed,
  validate = _validate
const id = 'network.habitat.relationship.writeTuple'

export type QueryParams = {}

export interface InputSchema {
  subject:
    | $Typed<NetworkHabitatRelationshipDefs.UserSubject>
    | $Typed<NetworkHabitatRelationshipDefs.SpaceRoleSubject>
    | { $type: string }
  /** Role granted on the object space (owner|manager|writer|reader). */
  relation: 'owner' | 'manager' | 'writer' | 'reader' | (string & {})
  object: NetworkHabitatRelationshipDefs.SpaceObject
}

export interface OutputSchema {
  /** URI of the written tuple record. */
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

export class SpaceNotFoundError extends XRPCError {
  constructor(src: XRPCError) {
    super(src.status, src.error, src.message, src.headers, { cause: src })
  }
}

export class InvalidTupleError extends XRPCError {
  constructor(src: XRPCError) {
    super(src.status, src.error, src.message, src.headers, { cause: src })
  }
}

export function toKnownErr(e: any) {
  if (e instanceof XRPCError) {
    if (e.error === 'SpaceNotFound') return new SpaceNotFoundError(e)
    if (e.error === 'InvalidTuple') return new InvalidTupleError(e)
  }

  return e
}
