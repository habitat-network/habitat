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
const id = 'network.habitat.permissions.listPermissions'

export interface Permission {
  $type?: 'network.habitat.permissions.listPermissions#permission'
  /** The grantee of the permission — either a DID or a habitat clique URI. */
  grantee: string
  /** The NSID of the collection the permission applies to. */
  collection: string
  /** The record key the permission applies to. Empty string means the permission covers the entire collection. */
  rkey?: string
  /** Whether this permission grants or denies access. */
  effect: 'allow' | 'deny' | (string & {})
}

const hashPermission = 'permission'

export function isPermission<V>(v: V) {
  return is$typed(v, id, hashPermission)
}

export function validatePermission<V>(v: V) {
  return validate<Permission & V>(v, id, hashPermission)
}

export type QueryParams = {}
export type InputSchema = undefined

export interface OutputSchema {
  permissions: Permission[]
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
