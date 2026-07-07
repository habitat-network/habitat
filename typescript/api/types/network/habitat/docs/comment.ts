/**
 * GENERATED CODE - DO NOT MODIFY
 */
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
const id = 'network.habitat.docs.comment'

export interface Main {
  $type: 'network.habitat.docs.comment'
  /** The comment text. */
  body: string
  /** When the comment was authored. */
  createdAt: string
  /** URI of the document space this comment relates to. */
  docSpace: string
  range?: Range
  /** Space-record URI of the parent comment this replies to. Omitted for top-level comments. */
  parent?: string
  [k: string]: unknown
}

const hashMain = 'main'

export function isMain<V>(v: V) {
  return is$typed(v, id, hashMain)
}

export function validateMain<V>(v: V) {
  return validate<Main & V>(v, id, hashMain, true)
}

export {
  type Main as Record,
  isMain as isRecord,
  validateMain as validateRecord,
}

export interface Range {
  $type?: 'network.habitat.docs.comment#range'
  /** JSON-encoded Yjs relative position of the range start. */
  start: string
  /** JSON-encoded Yjs relative position of the range end. */
  end: string
}

const hashRange = 'range'

export function isRange<V>(v: V) {
  return is$typed(v, id, hashRange)
}

export function validateRange<V>(v: V) {
  return validate<Range & V>(v, id, hashRange)
}
