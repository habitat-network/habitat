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
const id = 'network.habitat.space.listSpaces'

export type QueryParams = {
  /** Filter to spaces of this type. Required if the caller's OAuth scope is narrower than `space:*`. */
  type?: string
  /** Filter to spaces owned by this DID. Required if the caller's OAuth scope is narrower than `?did=*`. */
  did?: string
  /** The number of spaces to return. */
  limit?: number
  cursor?: string
}
export type InputSchema = undefined

export interface OutputSchema {
  cursor?: string
  spaces: SpaceView[]
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

export interface SpaceView {
  $type?: 'network.habitat.space.listSpaces#spaceView'
  /** URI of the space. */
  uri: string
  /** Whether the authenticated user is the owner of the space. */
  isOwner: boolean
}

const hashSpaceView = 'spaceView'

export function isSpaceView<V>(v: V) {
  return is$typed(v, id, hashSpaceView)
}

export function validateSpaceView<V>(v: V) {
  return validate<SpaceView & V>(v, id, hashSpaceView)
}
