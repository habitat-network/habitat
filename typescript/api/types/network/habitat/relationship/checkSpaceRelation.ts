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
const id = 'network.habitat.relationship.checkSpaceRelation'

export type QueryParams = {
  /** URI of the subject space (or group-space) whose role-holders form the userset to check. */
  subject: string
  /** The role held on the subject space, forming the userset. */
  subjectRole: 'owner' | 'manager' | 'writer' | 'reader'
  /** The role to check for on the space. */
  relation: 'owner' | 'manager' | 'writer' | 'reader'
  /** URI of the space. */
  space: string
}
export type InputSchema = undefined

export interface OutputSchema {
  /** Whether the subject userset holds the role on the space. */
  allowed: boolean
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
