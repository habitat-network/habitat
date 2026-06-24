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
const id = 'network.habitat.relationship.listGroups'

export type QueryParams = {
  /** URI of the space whose groups to list. */
  space: string
}
export type InputSchema = undefined

export interface OutputSchema {
  groups: GroupView[]
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

export interface GroupView {
  $type?: 'network.habitat.relationship.listGroups#groupView'
  /** URI of the group record. */
  uri: string
  name: string
  description?: string
  createdAt?: string
}

const hashGroupView = 'groupView'

export function isGroupView<V>(v: V) {
  return is$typed(v, id, hashGroupView)
}

export function validateGroupView<V>(v: V) {
  return validate<GroupView & V>(v, id, hashGroupView)
}
