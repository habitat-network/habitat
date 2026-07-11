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
const id = 'network.habitat.relationship.check'

export type QueryParams = {
  /** The subject to check: a user DID, or a space URI when checking a space-role userset. When a space URI, subjectRole is required. */
  subject: string
  /** The role held on the subject space, forming a userset. Required when subject is a space URI; omit when subject is a user DID. */
  subjectRole?: 'owner' | 'manager' | 'writer' | 'reader'
  /** The role to check for on the space. */
  relation: 'owner' | 'manager' | 'writer' | 'reader'
  /** URI of the space. */
  space: string
}
export type InputSchema = undefined

export interface OutputSchema {
  /** Whether the subject holds the role on the space. */
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
