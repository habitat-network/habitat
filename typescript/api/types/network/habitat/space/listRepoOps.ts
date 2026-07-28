/**
 * GENERATED CODE - DO NOT MODIFY
 */
import { type HeadersMap, XRPCError } from '@atproto/xrpc'
import { type ValidationResult, BlobRef } from '@atproto/lexicon'
import { CID } from 'multiformats/cid'
import { validate as _validate } from '../../../../lexicons.js'
import {
  type $Typed,
  is$typed as _is$typed,
  type OmitKey,
} from '../../../../util.js'
import type * as NetworkHabitatSpaceDefs from './defs.js'

const is$typed = _is$typed,
  validate = _validate
const id = 'network.habitat.space.listRepoOps'

export type QueryParams = {
  /** Reference to the space. */
  space: string
  /** The DID of the account whose oplog to retrieve. */
  repo: string
  /** Return operations after this revision. */
  since?: string
  /** Maximum number of operations to return. */
  limit?: number
  /** If true, omit inlined record values and return only operation metadata. */
  excludeValues?: boolean
}
export type InputSchema = undefined

export interface OutputSchema {
  ops: OpEntry[]
  commit?: NetworkHabitatSpaceDefs.SignedCommit
  cursor?: string
}

export interface CallOptions {
  signal?: AbortSignal
  headers?: HeadersMap
}

export interface Response {
  success: boolean
  headers: HeadersMap
  data: OutputSchema
}

export class SpaceNotFoundError extends XRPCError {
  constructor(src: XRPCError) {
    super(src.status, src.error, src.message, src.headers, { cause: src })
  }
}

export class RepoTakendownError extends XRPCError {
  constructor(src: XRPCError) {
    super(src.status, src.error, src.message, src.headers, { cause: src })
  }
}

export class RepoSuspendedError extends XRPCError {
  constructor(src: XRPCError) {
    super(src.status, src.error, src.message, src.headers, { cause: src })
  }
}

export class RepoDeactivatedError extends XRPCError {
  constructor(src: XRPCError) {
    super(src.status, src.error, src.message, src.headers, { cause: src })
  }
}

export function toKnownErr(e: any) {
  if (e instanceof XRPCError) {
    if (e.error === 'SpaceNotFound') return new SpaceNotFoundError(e)
    if (e.error === 'RepoTakendown') return new RepoTakendownError(e)
    if (e.error === 'RepoSuspended') return new RepoSuspendedError(e)
    if (e.error === 'RepoDeactivated') return new RepoDeactivatedError(e)
  }

  return e
}

/** A single operation in a permissioned repo's oplog. cid is null for deletes; prev is null for creates. Operations sharing the same rev belong to the same batch. value carries the record's current value for creates and updates, unless excludeValues was set or the value is stale (superseded by a later operation). */
export interface OpEntry {
  $type?: 'network.habitat.space.listRepoOps#opEntry'
  rev: string
  collection: string
  rkey: string
  cid: string | null
  prev: string | null
  /** The record's current value, inlined for create and update operations. Omitted when excludeValues is set, for deletes, or when the value has been superseded by a later operation. */
  value?: { [_ in string]: unknown }
}

const hashOpEntry = 'opEntry'

export function isOpEntry<V>(v: V) {
  return is$typed(v, id, hashOpEntry)
}

export function validateOpEntry<V>(v: V) {
  return validate<OpEntry & V>(v, id, hashOpEntry)
}
