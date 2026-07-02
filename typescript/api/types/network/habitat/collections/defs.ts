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
const id = 'network.habitat.collections.defs'

/** A record collection (lexicon type) present in the org's synced data, with a count of the distinct records in it the calling user can see. */
export interface CollectionView {
  $type?: 'network.habitat.collections.defs#collectionView'
  /** The NSID of the record collection. */
  collection: string
  /** Number of distinct records in this collection the calling user can see, counted across all spaces they can read. */
  recordCount: number
}

const hashCollectionView = 'collectionView'

export function isCollectionView<V>(v: V) {
  return is$typed(v, id, hashCollectionView)
}

export function validateCollectionView<V>(v: V) {
  return validate<CollectionView & V>(v, id, hashCollectionView)
}

/** A record identified by its repo, collection and rkey, together with the spaces it belongs to that the calling user can read. The record body is not included; fetch it on demand from pear with one of the spaces. */
export interface RecordView {
  $type?: 'network.habitat.collections.defs#recordView'
  /** The AT URI of the record (at://repo/collection/rkey). */
  uri: string
  /** DID of the repo the record lives in. */
  repo: string
  /** The NSID of the record collection. */
  collection: string
  /** The record key. */
  rkey: string
  /** URIs of the spaces this record belongs to that the calling user can read. */
  spaces: string[]
}

const hashRecordView = 'recordView'

export function isRecordView<V>(v: V) {
  return is$typed(v, id, hashRecordView)
}

export function validateRecordView<V>(v: V) {
  return validate<RecordView & V>(v, id, hashRecordView)
}
