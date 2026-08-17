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
const id = 'network.habitat.org.requestCrawl'

export type QueryParams = {}

export interface InputSchema {
  /** DID of the org to start syncing. */
  orgDid: string
}

export interface OutputSchema {
  /** authenticated: the org was newly authenticated via the JWT Bearer grant and crawling has started. alreadyRegistered: the org was already registered; no action taken. authError: authenticating the org credential failed (see message). */
  status: 'authenticated' | 'alreadyRegistered' | 'authError' | (string & {})
  /** Error detail, set when status is authError. */
  message?: string
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
