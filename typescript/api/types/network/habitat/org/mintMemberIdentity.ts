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

const is$typed = _is$typed,
  validate = _validate
const id = 'network.habitat.org.mintMemberIdentity'

export type QueryParams = {}

export interface InputSchema {
  /** The ID of the org this member is joining. */
  orgId?: string
  /** The internal handle (all letters + numbers, no special characters, does not include org domain) that will be used by the member. */
  handle: string
  /** The token that was issued by an org admin to allow members to join the organization.. */
  token: string
  /** The password for the new member's account. */
  password: string
}

export interface OutputSchema {
  /** The full handle of the newly minted member identity. */
  handle: string
  /** The DID of the newly minted member identity. */
  did: string
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
