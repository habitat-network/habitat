/**
 * GENERATED CODE - DO NOT MODIFY
 */
import { HeadersMap, XRPCError } from '@atproto/xrpc'
import { type ValidationResult, BlobRef } from '@atproto/lexicon'
import { CID } from 'multiformats/cid'
import { validate as _validate } from '../../../lexicons'
import { type $Typed, is$typed as _is$typed, type OmitKey } from '../../../util'

const is$typed = _is$typed,
  validate = _validate
const id = 'network.habitat.listConnectedApps'

export type QueryParams = {}
export type InputSchema = undefined

export interface OutputSchema {
  apps: App[]
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

export interface App {
  $type?: 'network.habitat.listConnectedApps#app'
  /** The name of this app. */
  name: string
  /** The ID of this app. */
  clientID: string
  /** The uri of this app. */
  clientUri: string
  /** The last time habitat detected a session with this app. */
  lastUsed: string
  /** The logo URI of this app. */
  logoUri?: string
}

const hashApp = 'app'

export function isApp<V>(v: V) {
  return is$typed(v, id, hashApp)
}

export function validateApp<V>(v: V) {
  return validate<App & V>(v, id, hashApp)
}
