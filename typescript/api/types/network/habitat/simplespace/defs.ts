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
const id = 'network.habitat.simplespace.defs'

/** Configuration for a space managed by the simplespace implementation. A credential is minted only when the user is authorized by `policy` and their app by `appAccess`. */
export interface SpaceConfig {
  $type?: 'network.habitat.simplespace.defs#spaceConfig'
  /** How the authority decides whether to authorize a requesting user. 'member-list' (default) consults the member list, 'public' authorizes anyone, 'managing-app' asks the managingApp via checkUserAccess. */
  policy: 'public' | 'member-list' | 'managing-app' | (string & {})
  appAccess: $Typed<Open> | $Typed<AllowList> | { $type: string }
  /** Service identifier (e.g. did:web:example.com#forum) of the app that manages this space. Routes application-level requests and is the checkUserAccess target when policy is 'managing-app'. */
  managingApp?: string
}

const hashSpaceConfig = 'spaceConfig'

export function isSpaceConfig<V>(v: V) {
  return is$typed(v, id, hashSpaceConfig)
}

export function validateSpaceConfig<V>(v: V) {
  return validate<SpaceConfig & V>(v, id, hashSpaceConfig)
}

/** App access policy: any app may access the space. No client attestation required. */
export interface Open {
  $type?: 'network.habitat.simplespace.defs#open'
}

const hashOpen = 'open'

export function isOpen<V>(v: V) {
  return is$typed(v, id, hashOpen)
}

export function validateOpen<V>(v: V) {
  return validate<Open & V>(v, id, hashOpen)
}

/** App access policy: only the named clients may access the space, evaluated against the attested client_id. */
export interface AllowList {
  $type?: 'network.habitat.simplespace.defs#allowList'
  /** The OAuth client IDs permitted to access the space. */
  allowed: string[]
}

const hashAllowList = 'allowList'

export function isAllowList<V>(v: V) {
  return is$typed(v, id, hashAllowList)
}

export function validateAllowList<V>(v: V) {
  return validate<AllowList & V>(v, id, hashAllowList)
}
