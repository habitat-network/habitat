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
import type * as NetworkHabitatGrantee from '../grantee.js'

const is$typed = _is$typed,
  validate = _validate
const id = 'network.habitat.permissions.removePermission'

export type QueryParams = {}

export interface InputSchema {
  grantees: (
    | $Typed<NetworkHabitatGrantee.DidGrantee>
    | $Typed<NetworkHabitatGrantee.CliqueRef>
    | { $type: string }
  )[]
  /** The NSID of the lexicon or record to grant read permission for. */
  collection: string
  /** The Record Key to grant read permissions to, if any. */
  rkey?: string
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
}

export function toKnownErr(e: any) {
  return e
}
