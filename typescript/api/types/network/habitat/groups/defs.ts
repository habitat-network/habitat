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
const id = 'network.habitat.groups.defs'

/** A group, backed by a network.habitat.group space, with its membership resolved. Membership is the set of users holding at least the writer role on the group-space, expanded through inherited groups. */
export interface GroupView {
  $type?: 'network.habitat.groups.defs#groupView'
  /** URI of the group-space. */
  uri: string
  name: string
  description?: string
  createdAt?: string
  /** Number of distinct members after expanding inherited groups. */
  memberCount?: number
  /** Whether the calling user is a member of this group. */
  isMember: boolean
  /** Whether the calling user can manage this group (add members, edit it). */
  canManage: boolean
  members?: MemberView[]
  /** Other groups this group inherits members from. */
  inheritedGroups?: GroupRef[]
}

const hashGroupView = 'groupView'

export function isGroupView<V>(v: V) {
  return is$typed(v, id, hashGroupView)
}

export function validateGroupView<V>(v: V) {
  return validate<GroupView & V>(v, id, hashGroupView)
}

export interface MemberView {
  $type?: 'network.habitat.groups.defs#memberView'
  did: string
  /** Role held on the group-space (owner|manager|writer|reader). */
  role?: string
  /** True if the member is granted a role directly on this group, false if the membership is inherited from another group. */
  direct: boolean
  /** If inherited, the URI of the group-space the membership came from. */
  viaGroup?: string
}

const hashMemberView = 'memberView'

export function isMemberView<V>(v: V) {
  return is$typed(v, id, hashMemberView)
}

export function validateMemberView<V>(v: V) {
  return validate<MemberView & V>(v, id, hashMemberView)
}

export interface GroupRef {
  $type?: 'network.habitat.groups.defs#groupRef'
  uri: string
  name: string
}

const hashGroupRef = 'groupRef'

export function isGroupRef<V>(v: V) {
  return is$typed(v, id, hashGroupRef)
}

export function validateGroupRef<V>(v: V) {
  return validate<GroupRef & V>(v, id, hashGroupRef)
}
