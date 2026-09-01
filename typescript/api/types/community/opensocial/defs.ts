/**
 * GENERATED CODE - DO NOT MODIFY
 */
import { type ValidationResult, BlobRef } from '@atproto/lexicon'
import { CID } from 'multiformats/cid'
import { validate as _validate } from '../../../lexicons.js'
import {
  type $Typed,
  is$typed as _is$typed,
  type OmitKey,
} from '../../../util.js'

const is$typed = _is$typed,
  validate = _validate
const id = 'community.opensocial.defs'

/** A pending invite to join the community, tracked in the org's invite table (not yet a repo record). */
export interface InviteView {
  $type?: 'community.opensocial.defs#inviteView'
  /** Opaque id of the invite, used to revoke it. */
  id: string
  /** DID of the community the invite is for. */
  org: string
  /** DID of the invited user. */
  invitee: string
  /** Record keys of the community.opensocial.role records the invitee will hold once they accept. */
  roles: string[]
  createdAt: string
}

const hashInviteView = 'inviteView'

export function isInviteView<V>(v: V) {
  return is$typed(v, id, hashInviteView)
}

export function validateInviteView<V>(v: V) {
  return validate<InviteView & V>(v, id, hashInviteView)
}
