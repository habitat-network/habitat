/**
 * GENERATED CODE - DO NOT MODIFY
 */
import { type ValidationResult, BlobRef } from '@atproto/lexicon'
import { CID } from 'multiformats/cid'
import { validate as _validate } from '../../../lexicons'
import { type $Typed, is$typed as _is$typed, type OmitKey } from '../../../util'

const is$typed = _is$typed,
  validate = _validate
const id = 'network.habitat.grantee'

/** A DID grantee */
export interface DidGrantee {
  $type?: 'network.habitat.grantee#didGrantee'
  did: string
}

const hashDidGrantee = 'didGrantee'

export function isDidGrantee<V>(v: V) {
  return is$typed(v, id, hashDidGrantee)
}

export function validateDidGrantee<V>(v: V) {
  return validate<DidGrantee & V>(v, id, hashDidGrantee)
}

/** A clique ref grantee in the form habitat://did:plc:web:arushi/habitat.network.clique/clique-record-key */
export interface CliqueRef {
  $type?: 'network.habitat.grantee#cliqueRef'
  uri: string
}

const hashCliqueRef = 'cliqueRef'

export function isCliqueRef<V>(v: V) {
  return is$typed(v, id, hashCliqueRef)
}

export function validateCliqueRef<V>(v: V) {
  return validate<CliqueRef & V>(v, id, hashCliqueRef)
}
