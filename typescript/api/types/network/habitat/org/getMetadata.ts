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
const id = 'network.habitat.org.getMetadata'

export type QueryParams = {
  /** The orge ID of the organization to look up. If not specified, defaults to the authenticated caller's org. */
  orgId?: string
}
export type InputSchema = undefined

export interface OutputSchema {
  /** The name of this organization. */
  name?: string
  /** A description for this organization. */
  description?: string
  /** Login method for the org: 'password', 'atproto', or 'google'. */
  loginMethod: string
  /** The subdomain used for all org member handles. */
  handleSubdomain: string
  /** The unique ID of this organization. */
  orgId: string
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
