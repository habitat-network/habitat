/**
 * GENERATED CODE - DO NOT MODIFY
 */
import { type HeadersMap, XRPCError } from '@atproto/xrpc'
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
const id = 'network.habitat.relationship.listRelations'

export type QueryParams = {
  /** URI of the governing space whose relations to list. */
  space: string
  /** Optional. Restrict to relations whose object is this space or group URI. */
  object?: string
  /** Optional. Restrict to relations whose subject is this user DID. */
  subjectDid?: string
  /** Optional. Restrict to relations whose subject is a user (userRelation) or a space userset (spaceRelation). */
  subjectType?: 'user' | 'space'
  /** Optional. Restrict to relations with this relation. */
  relation?: string
}
export type InputSchema = undefined

export interface OutputSchema {
  relations: (
    $Typed<UserRelationView> | $Typed<SpaceRelationView> | { $type: string }
  )[]
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

/** A user relation record together with its URI. */
export interface UserRelationView {
  $type?: 'network.habitat.relationship.listRelations#userRelationView'
  /** URI of the relation record. */
  uri: string
  subject: NetworkHabitatRelationshipDefs.UserSubject
  relation: string
  object: NetworkHabitatRelationshipDefs.SpaceObject
}

const hashUserRelationView = 'userRelationView'

export function isUserRelationView<V>(v: V) {
  return is$typed(v, id, hashUserRelationView)
}

export function validateUserRelationView<V>(v: V) {
  return validate<UserRelationView & V>(v, id, hashUserRelationView)
}

/** A space relation record together with its URI. */
export interface SpaceRelationView {
  $type?: 'network.habitat.relationship.listRelations#spaceRelationView'
  /** URI of the relation record. */
  uri: string
  subject: NetworkHabitatRelationshipDefs.SpaceRoleSubject
  relation: string
  object: NetworkHabitatRelationshipDefs.SpaceObject
}

const hashSpaceRelationView = 'spaceRelationView'

export function isSpaceRelationView<V>(v: V) {
  return is$typed(v, id, hashSpaceRelationView)
}

export function validateSpaceRelationView<V>(v: V) {
  return validate<SpaceRelationView & V>(v, id, hashSpaceRelationView)
}
