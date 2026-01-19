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
const id = 'community.lexicon.location.address'

/** A physical location in the form of a street address. */
export interface Main {
  $type?: 'community.lexicon.location.address'
  /** The ISO 3166 country code. Preferably the 2-letter code. */
  country: string
  /** The postal code of the location. */
  postalCode?: string
  /** The administrative region of the country. For example, a state in the USA. */
  region?: string
  /** The locality of the region. For example, a city in the USA. */
  locality?: string
  /** The street address. */
  street?: string
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
