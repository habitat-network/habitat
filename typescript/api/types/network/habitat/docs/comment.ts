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
  /** Yjs relative position (Y.encodeRelativePosition, applied to the doc's 'default' XML fragment) marking the start of the commented range. Together with anchorEnd and the document's current CRDT state, this is sufficient to resolve the exact range being commented on — an editor placing this comment needs no other context, and the position survives concurrent edits made anywhere else in the document the way a plain character offset would not. */
  anchorStart: Uint8Array
  /** Yjs relative position marking the end of the commented range. See anchorStart. */
  anchorEnd: Uint8Array
  /** A snapshot of the document text the anchor pointed to when the comment was created, shown as a fallback if the anchor no longer resolves to a valid range (e.g. the text was later deleted entirely). */
  quotedText?: string
  /** When the comment was written. */
  createdAt: string
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
