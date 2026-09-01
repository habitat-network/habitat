/**
 * GENERATED CODE - DO NOT MODIFY
 */
import { type HeadersMap, XRPCError } from '@atproto/xrpc'
import { type ValidationResult, BlobRef } from '@atproto/lexicon'
import { CID } from 'multiformats/cid'
import { validate as _validate } from '../../../lexicons.js'
import {
  type $Typed,
  is$typed as _is$typed,
  type OmitKey,
} from '../../../util.js'
import type * as CommunityOpensocialDefs from './defs.js'

const is$typed = _is$typed,
  validate = _validate
const id = 'community.opensocial.createInvite'

export type QueryParams = {}

export interface InputSchema {
  /** DID of the community to invite into. */
  org: string
  /** DID of the user to invite. */
  invitee: string
  /** Record keys of the community.opensocial.role records to grant the invitee once they accept. Defaults to no roles. */
  roles?: string[]
}

export interface OutputSchema {
  invite: CommunityOpensocialDefs.InviteView
}

export interface CallOptions {
  signal?: AbortSignal
  headers?: HeadersMap
  qp?: QueryParams
  encoding?: 'application/json'
}

export interface Response {
  success: boolean
  headers: HeadersMap
  data: OutputSchema
}

export class AlreadyMemberError extends XRPCError {
  constructor(src: XRPCError) {
    super(src.status, src.error, src.message, src.headers, { cause: src })
  }
}

export class InviteAlreadyExistsError extends XRPCError {
  constructor(src: XRPCError) {
    super(src.status, src.error, src.message, src.headers, { cause: src })
  }
}

export function toKnownErr(e: any) {
  if (e instanceof XRPCError) {
    if (e.error === 'AlreadyMember') return new AlreadyMemberError(e)
    if (e.error === 'InviteAlreadyExists')
      return new InviteAlreadyExistsError(e)
  }

  return e
}
