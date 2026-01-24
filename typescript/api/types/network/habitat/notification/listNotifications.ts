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
import type * as NetworkHabitatNotificationDefs from './defs.js'

const is$typed = _is$typed,
  validate = _validate
const id = 'network.habitat.notification.listNotifications'

export type QueryParams = {
  /** The NSID of the record type. */
  collection?: string
}
export type InputSchema = undefined

export interface OutputSchema {
  cursor?: string
  records: Record[]
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

export interface Record {
  $type?: 'network.habitat.notification.listNotifications#record'
  uri: string
  cid: string
  value: NetworkHabitatNotificationDefs.Notification
}

const hashRecord = 'record'

export function isRecord<V>(v: V) {
  return is$typed(v, id, hashRecord)
}

export function validateRecord<V>(v: V) {
  return validate<Record & V>(v, id, hashRecord)
}
