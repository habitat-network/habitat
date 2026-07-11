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
const id = 'network.habitat.relationship.listObjects'

export type QueryParams = {
  /** DID of the user. */
  did: string
  /** The role to query for. */
  relation: 'owner' | 'manager' | 'writer' | 'reader'
  /** Filter to spaces of this type. */
  type?: string
}
export type InputSchema = undefined

export interface OutputSchema {
  /** URIs of spaces where the user holds the role. */
  spaces: string[]
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
