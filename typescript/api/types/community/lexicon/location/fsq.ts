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
const id = 'community.lexicon.location.fsq'

/** A physical location contained in the Foursquare Open Source Places dataset. */
export interface Main {
  $type?: 'community.lexicon.location.fsq'
  /** The unique identifier of a Foursquare POI. */
  fsq_place_id: string
  latitude?: string
  longitude?: string
  /** The name of the location. */
  name?: string
}

const hashMain = 'main'

export function isMain<V>(v: V) {
  return is$typed(v, id, hashMain)
}

export function validateMain<V>(v: V) {
  return validate<Main & V>(v, id, hashMain)
}
