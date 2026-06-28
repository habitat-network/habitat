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
const id = 'network.habitat.relationship.defs'

/** A space that a role is granted on. */
export interface SpaceObject {
  $type?: 'network.habitat.relationship.defs#spaceObject'
  /** URI of the space. */
  space: string
}

const hashSpaceObject = 'spaceObject'

export function isSpaceObject<V>(v: V) {
  return is$typed(v, id, hashSpaceObject)
}

export function validateSpaceObject<V>(v: V) {
  return validate<SpaceObject & V>(v, id, hashSpaceObject)
}

/** An individual user, identified by DID. */
export interface UserSubject {
  $type?: 'network.habitat.relationship.defs#userSubject'
  did: string
}

const hashUserSubject = 'userSubject'

export function isUserSubject<V>(v: V) {
  return is$typed(v, id, hashUserSubject)
}

export function validateUserSubject<V>(v: V) {
  return validate<UserSubject & V>(v, id, hashUserSubject)
}

/** All subjects holding a role on a space (a userset). Enables cross-space inheritance, e.g. spaceA's writers as writers of spaceB. */
export interface SpaceRoleSubject {
  $type?: 'network.habitat.relationship.defs#spaceRoleSubject'
  /** URI of the space (or group-space). */
  space: string
  role: 'owner' | 'manager' | 'writer' | 'reader'
}

const hashSpaceRoleSubject = 'spaceRoleSubject'

export function isSpaceRoleSubject<V>(v: V) {
  return is$typed(v, id, hashSpaceRoleSubject)
}

export function validateSpaceRoleSubject<V>(v: V) {
  return validate<SpaceRoleSubject & V>(v, id, hashSpaceRoleSubject)
}
