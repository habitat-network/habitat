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
const id = 'network.habitat.docs.listDocs'

export type QueryParams = {}
export type InputSchema = undefined

export interface OutputSchema {
  docs: DocView[]
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

export interface DocView {
  $type?: 'network.habitat.docs.listDocs#docView'
  /** The doc's space key, used as the document identifier in updateDoc and routing. */
  docId: string
  /** URI of the doc's space. */
  uri: string
  /** The document title, from its markdown 'self' record. */
  title: string
  /** URI of the document's companion comment space, where comment records are written. */
  commentSpace?: string
}

const hashDocView = 'docView'

export function isDocView<V>(v: V) {
  return is$typed(v, id, hashDocView)
}

export function validateDocView<V>(v: V) {
  return validate<DocView & V>(v, id, hashDocView)
}
