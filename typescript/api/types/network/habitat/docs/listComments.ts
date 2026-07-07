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
const id = 'network.habitat.docs.listComments'

export type QueryParams = {
  /** The document's space key. */
  docId: string
}
export type InputSchema = undefined

export interface OutputSchema {
  comments: CommentView[]
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

export interface CommentView {
  $type?: 'network.habitat.docs.listComments#commentView'
  /** The comment record's space-record URI; used as the parent reference when replying. */
  uri: string
  /** DID of the comment's author. */
  author: string
  body: string
  createdAt: string
  range?: RangeView
  /** Direct replies to this comment, oldest first. */
  replies: ReplyView[]
}

const hashCommentView = 'commentView'

export function isCommentView<V>(v: V) {
  return is$typed(v, id, hashCommentView)
}

export function validateCommentView<V>(v: V) {
  return validate<CommentView & V>(v, id, hashCommentView)
}

export interface ReplyView {
  $type?: 'network.habitat.docs.listComments#replyView'
  uri: string
  author: string
  body: string
  createdAt: string
}

const hashReplyView = 'replyView'

export function isReplyView<V>(v: V) {
  return is$typed(v, id, hashReplyView)
}

export function validateReplyView<V>(v: V) {
  return validate<ReplyView & V>(v, id, hashReplyView)
}

export interface RangeView {
  $type?: 'network.habitat.docs.listComments#rangeView'
  start: string
  end: string
}

const hashRangeView = 'rangeView'

export function isRangeView<V>(v: V) {
  return is$typed(v, id, hashRangeView)
}

export function validateRangeView<V>(v: V) {
  return validate<RangeView & V>(v, id, hashRangeView)
}
