/**
 * GENERATED CODE - DO NOT MODIFY
 */
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
const id = 'network.habitat.greensky.post'

export interface Main {
  $type: 'network.habitat.greensky.post'
  /** The primary post content. */
  text: string
  /** Client-declared timestamp when this post was originally created. */
  createdAt: string
  reply?: ReplyRef
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

export interface ReplyRef {
  $type?: 'network.habitat.greensky.post#replyRef'
  /** Record URI of the thread's root post. */
  root: string
  /** Record URI of the post being replied to. */
  parent: string
}

const hashReplyRef = 'replyRef'

export function isReplyRef<V>(v: V) {
  return is$typed(v, id, hashReplyRef)
}

export function validateReplyRef<V>(v: V) {
  return validate<ReplyRef & V>(v, id, hashReplyRef)
}
