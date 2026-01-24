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
const id = 'network.habitat.notification.defs'

export interface Notification {
  $type?: 'network.habitat.notification.defs#notification'
  /** The handle or DID of the target of the notification. */
  did: string
  /** The handle or DID of the origin of the notification. */
  originDid: string
  /** The NSID of the record collection. */
  collection: string
  /** The Record Key. */
  rkey: string
}

const hashNotification = 'notification'

export function isNotification<V>(v: V) {
  return is$typed(v, id, hashNotification)
}

export function validateNotification<V>(v: V) {
  return validate<Notification & V>(v, id, hashNotification)
}
