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
import type * as ComAtprotoRepoStrongRef from '../../../com/atproto/repo/strongRef.js'

const is$typed = _is$typed,
  validate = _validate
const id = 'community.lexicon.calendar.rsvp'

export interface Main {
  $type: 'community.lexicon.calendar.rsvp'
  subject: ComAtprotoRepoStrongRef.Main
  status:
    | 'community.lexicon.calendar.rsvp#interested'
    | 'community.lexicon.calendar.rsvp#going'
    | 'community.lexicon.calendar.rsvp#notgoing'
    | (string & {})
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

/** Interested in the event */
export const INTERESTED = `${id}#interested`
/** Going to the event */
export const GOING = `${id}#going`
/** Not going to the event */
export const NOTGOING = `${id}#notgoing`
