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
import type * as NetworkHabitatRelationshipDefs from './defs.js'

const is$typed = _is$typed,
  validate = _validate
const id = 'network.habitat.relationship.tuple'

export interface Main {
  $type: 'network.habitat.relationship.tuple'
  subject:
    | $Typed<NetworkHabitatRelationshipDefs.UserSubject>
    | $Typed<NetworkHabitatRelationshipDefs.SpaceRoleSubject>
    | { $type: string }
  /** Role granted on the object space (owner|manager|writer|reader). */
  relation: 'owner' | 'manager' | 'writer' | 'reader' | (string & {})
  object: NetworkHabitatRelationshipDefs.SpaceObject
  createdAt?: string
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
