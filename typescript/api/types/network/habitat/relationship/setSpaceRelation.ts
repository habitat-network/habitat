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
const id = 'network.habitat.relationship.setSpaceRelation'

export type QueryParams = {}

export interface InputSchema {
  /** URI of the subject space (or group-space) whose role-holders form the userset to grant the role to. */
  subject: string
  /** The role held on the subject space, forming the userset. */
  subjectRole: 'owner' | 'manager' | 'writer' | 'reader'
  /** Role granted on the space (owner|manager|writer|reader). */
  relation: 'owner' | 'manager' | 'writer' | 'reader' | (string & {})
  /** URI of the space to grant the role on. */
  space: string
}

export interface OutputSchema {
  /** URI of the written relation record. */
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

export class InvalidRelationError extends XRPCError {
  constructor(src: XRPCError) {
    super(src.status, src.error, src.message, src.headers, { cause: src })
  }
}

export function toKnownErr(e: any) {
  if (e instanceof XRPCError) {
    if (e.error === 'SpaceNotFound') return new SpaceNotFoundError(e)
    if (e.error === 'InvalidRelation') return new InvalidRelationError(e)
  }

  return e
}
