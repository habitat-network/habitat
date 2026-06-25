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
const id = 'network.habitat.greensky.getPosts'

export type QueryParams = {}
export type InputSchema = undefined

export interface OutputSchema {
  threads: ThreadView[]
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

export interface ThreadView {
  $type?: 'network.habitat.greensky.getPosts#threadView'
  post: PostView
  /** Replies to the root post, oldest first. */
  replies: PostView[]
}

const hashThreadView = 'threadView'

export function isThreadView<V>(v: V) {
  return is$typed(v, id, hashThreadView)
}

export function validateThreadView<V>(v: V) {
  return validate<ThreadView & V>(v, id, hashThreadView)
}

export interface PostView {
  $type?: 'network.habitat.greensky.getPosts#postView'
  /** Record URI of the post. */
  uri: string
  /** URI of the space (thread) the post belongs to. Replies are written into this same space. */
  spaceUri: string
  /** DID of the post author. */
  author: string
  text: string
  /** Client-declared creation timestamp. */
  createdAt: string
  /** When the greensky server ingested this post from sap. */
  indexedAt?: string
}

const hashPostView = 'postView'

export function isPostView<V>(v: V) {
  return is$typed(v, id, hashPostView)
}

export function validatePostView<V>(v: V) {
  return validate<PostView & V>(v, id, hashPostView)
}
