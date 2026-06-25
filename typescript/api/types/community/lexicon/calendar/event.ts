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
import type * as CommunityLexiconLocationAddress from '../location/address.js'
import type * as CommunityLexiconLocationFsq from '../location/fsq.js'
import type * as CommunityLexiconLocationGeo from '../location/geo.js'
import type * as CommunityLexiconLocationHthree from '../location/hthree.js'

const is$typed = _is$typed,
  validate = _validate
const id = 'community.lexicon.calendar.event'

export interface Main {
  $type: 'community.lexicon.calendar.event'
  /** The name of the event. */
  name: string
  /** The description of the event. */
  description?: string
  /** Client-declared timestamp when the event was created. */
  createdAt: string
  /** Client-declared timestamp when the event starts. */
  startsAt?: string
  /** Client-declared timestamp when the event ends. */
  endsAt?: string
  mode?: Mode
  status?: Status
  /** The locations where the event takes place. */
  locations?: (
    | $Typed<Uri>
    | $Typed<CommunityLexiconLocationAddress.Main>
    | $Typed<CommunityLexiconLocationFsq.Main>
    | $Typed<CommunityLexiconLocationGeo.Main>
    | $Typed<CommunityLexiconLocationHthree.Main>
    | { $type: string }
  )[]
  /** URIs associated with the event. */
  uris?: Uri[]
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

/** The mode of the event. */
export type Mode =
  | 'community.lexicon.calendar.event#hybrid'
  | 'community.lexicon.calendar.event#inperson'
  | 'community.lexicon.calendar.event#virtual'
  | (string & {})

/** A virtual event that takes place online. */
export const VIRTUAL = `${id}#virtual`
/** An in-person event that takes place offline. */
export const INPERSON = `${id}#inperson`
/** A hybrid event that takes place both online and offline. */
export const HYBRID = `${id}#hybrid`

/** The status of the event. */
export type Status =
  | 'community.lexicon.calendar.event#cancelled'
  | 'community.lexicon.calendar.event#planned'
  | 'community.lexicon.calendar.event#postponed'
  | 'community.lexicon.calendar.event#rescheduled'
  | 'community.lexicon.calendar.event#scheduled'
  | (string & {})

/** The event has been created, but not finalized. */
export const PLANNED = `${id}#planned`
/** The event has been created and scheduled. */
export const SCHEDULED = `${id}#scheduled`
/** The event has been rescheduled. */
export const RESCHEDULED = `${id}#rescheduled`
/** The event has been cancelled. */
export const CANCELLED = `${id}#cancelled`
/** The event has been postponed and a new start date has not been set. */
export const POSTPONED = `${id}#postponed`

/** A URI associated with the event. */
export interface Uri {
  $type?: 'community.lexicon.calendar.event#uri'
  uri: string
  /** The display name of the URI. */
  name?: string
}

const hashUri = 'uri'

export function isUri<V>(v: V) {
  return is$typed(v, id, hashUri)
}

export function validateUri<V>(v: V) {
  return validate<Uri & V>(v, id, hashUri)
}
