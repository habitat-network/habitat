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

/** A clique grantee in the form clique:did:plc:web:arushi/clique-key */
export interface Clique {
  $type?: 'network.habitat.grantee#clique'
  clique: string
}

const hashClique = 'clique'

export function isClique<V>(v: V) {
  return is$typed(v, id, hashClique)
}

export function validateClique<V>(v: V) {
  return validate<Clique & V>(v, id, hashClique)
}
