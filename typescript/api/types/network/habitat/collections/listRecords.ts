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
import type * as NetworkHabitatCollectionsDefs from './defs.js'

const is$typed = _is$typed,
  validate = _validate
const id = 'network.habitat.collections.listRecords'

export type QueryParams = {
  /** The NSID of the record collection to list. */
  collection: string
}
export type InputSchema = undefined

export interface OutputSchema {
  records: NetworkHabitatCollectionsDefs.RecordView[]
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
