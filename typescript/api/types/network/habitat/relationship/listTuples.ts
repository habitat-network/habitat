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
import type * as NetworkHabitatRelationshipDefs from './defs.js'

const is$typed = _is$typed,
  validate = _validate
const id = 'network.habitat.relationship.listTuples'

export type QueryParams = {
  /** URI of the governing space whose tuples to list. */
  space: string
  /** Optional. Restrict to tuples whose object is this space or group URI. */
  object?: string
  /** Optional. Restrict to tuples whose subject is this user DID. */
  subjectDid?: string
  /** Optional. Restrict to tuples with this relation. */
  relation?: string
}
export type InputSchema = undefined

export interface OutputSchema {
  tuples: TupleView[]
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

export interface TupleView {
  $type?: 'network.habitat.relationship.listTuples#tupleView'
  /** URI of the tuple record. */
  uri: string
  subject:
    | $Typed<NetworkHabitatRelationshipDefs.UserSubject>
    | $Typed<NetworkHabitatRelationshipDefs.SpaceRoleSubject>
    | { $type: string }
  relation: string
  object: NetworkHabitatRelationshipDefs.SpaceObject
}

const hashTupleView = 'tupleView'

export function isTupleView<V>(v: V) {
  return is$typed(v, id, hashTupleView)
}

export function validateTupleView<V>(v: V) {
  return validate<TupleView & V>(v, id, hashTupleView)
}
