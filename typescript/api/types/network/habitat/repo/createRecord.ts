/**
 * GENERATED CODE - DO NOT MODIFY
 */
import { HeadersMap, XRPCError } from '@atproto/xrpc'
import { type ValidationResult, BlobRef } from '@atproto/lexicon'
import { CID } from 'multiformats/cid'
import { validate as _validate } from '../../../../lexicons'
import {
  type $Typed,
  is$typed as _is$typed,
  type OmitKey,
} from '../../../../util'
import type * as NetworkHabitatGrantee from '../grantee.js'

const is$typed = _is$typed,
  validate = _validate
const id = 'network.habitat.repo.createRecord'

export type QueryParams = {}

export interface InputSchema {
  /** The handle or DID of the repo (aka, current account). */
  repo: string
  /** The NSID of the record collection. */
  collection: string
  /** The Record Key. */
  rkey?: string
  /** Can be set to 'false' to skip Lexicon schema validation of record data, 'true' to require it, or leave unset to validate only for known Lexicons. */
  validate?: boolean
  /** The record to write. */
  record: { [_ in string]: unknown }
  /** Whether to create a clique with the given grantees. If true, all grantees must be DIDs, and the created clique ref is returned. */
  createGranteesClique?: boolean
  /** Any grantees to set for this record */
  grantees?: (
    | $Typed<NetworkHabitatGrantee.DidGrantee>
    | $Typed<NetworkHabitatGrantee.Clique>
    | { $type: string }
  )[]
}

export interface OutputSchema {
  /** The habitat-uri of the put-ed object. */
  uri: string
  validationStatus?: 'valid' | 'unknown' | (string & {})
  /** If a clique was created, return its ref, formatted like clique:<owner did>/<clique key> */
  clique?: string
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

export function toKnownErr(e: any) {
  return e
}
