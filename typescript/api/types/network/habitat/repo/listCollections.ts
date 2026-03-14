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
const id = 'network.habitat.repo.listCollections'

export type QueryParams = {}
export type InputSchema = undefined

export interface OutputSchema {
  collections: CollectionMetadata[]
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

export function toKnownErr(e: any) {
  return e
}

export interface CollectionMetadata {
  $type?: 'network.habitat.repo.listCollections#collectionMetadata'
  /** The NSID of this collection, */
  nsid: string
  /** Number of records for this collection. */
  recordCount: number
  /** The last time a record in this collection was touched. */
  lastTouched: string
  grantees: (
    | $Typed<NetworkHabitatGrantee.DidGrantee>
    | $Typed<NetworkHabitatGrantee.Clique>
    | { $type: string }
  )[]
}

const hashCollectionMetadata = 'collectionMetadata'

export function isCollectionMetadata<V>(v: V) {
  return is$typed(v, id, hashCollectionMetadata)
}

export function validateCollectionMetadata<V>(v: V) {
  return validate<CollectionMetadata & V>(v, id, hashCollectionMetadata)
}
