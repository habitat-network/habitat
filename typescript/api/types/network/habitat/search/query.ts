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
const id = 'network.habitat.search.query'

export type QueryParams = {
  /** The search query text. */
  q: string
  limit?: number
  cursor?: string
}
export type InputSchema = undefined

export interface OutputSchema {
  results: ResultView[]
  cursor?: string
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

export interface ResultView {
  $type?: 'network.habitat.search.query#resultView'
  /** URI of the matched record. */
  uri: string
  /** URI of the space the record belongs to. */
  spaceUri: string
  /** The NSID of the record type. */
  recordType: string
  /** A highlighted excerpt of the matching content. */
  snippet?: string
  /** Relevance score scaled by 1,000,000, higher is more relevant. */
  rank?: number
}

const hashResultView = 'resultView'

export function isResultView<V>(v: V) {
  return is$typed(v, id, hashResultView)
}

export function validateResultView<V>(v: V) {
  return validate<ResultView & V>(v, id, hashResultView)
}
