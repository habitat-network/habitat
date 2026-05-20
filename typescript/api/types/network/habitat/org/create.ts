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
const id = 'network.habitat.org.create'

export type QueryParams = {}

export interface InputSchema {
  /** Internal handle for the bootstrap admin (alphanumeric, 1-50 chars). */
  admin_handle: string
  /** Password for the bootstrap admin account (required for password login method). */
  admin_password?: string
  /** Subdomain for all org member handles (e.g. 'acmecorp'). */
  handle_subdomain?: string
  /** A display name for this org. */
  name?: string
  /** Login method for the org: 'password', 'atproto', or 'google'. */
  login_method?: string
  /** Provider-specific identifier (public ATProto DID for 'atproto', email for 'google'). Ignored for 'password'. */
  login_id?: string
}

export interface OutputSchema {
  /** The ID of the created org. */
  org_id: string
  /** The DID of the bootstrap admin. */
  admin_did: string
  /** The full handle of the bootstrap admin. */
  admin_handle: string
  /** The display name of the created org. */
  name: string
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

export function toKnownErr(e: any) {
  return e
}
